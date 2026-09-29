package artifact

import (
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
)

// uploadFormField is the multipart field carrying the file.
const uploadFormField = "file"

// uploadSlackBytes is what the request may exceed the file limit by: multipart
// boundaries, part headers, and the trailing epilogue.
const uploadSlackBytes int64 = 1 << 20

// maxTitleLen bounds the caller-supplied label. It is display text, and a
// durable column is not a place for an unbounded string.
const maxTitleLen = 200

func (h *Handler) uploadArtifactHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, svc, ok := h.spaceCaller(w, r)
	if !ok {
		return
	}
	ReceiveUpload(w, r, svc, ReceiveInput{
		SpaceID:       spaceID,
		SourceType:    coreartifact.SourceUserUpload,
		CreatedByType: coreartifact.CreatorUser,
		CreatedByID:   userID,
		Share:         WantShare(r),
	})
}

func (h *Handler) uploadToDefaultSpaceHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndDefaultSpace(w, r, r.URL.Query().Get("space_id"))
	if !ok {
		return
	}
	svc, ok := h.service(w)
	if !ok {
		return
	}
	ReceiveUpload(w, r, svc, ReceiveInput{
		SpaceID:       spaceID,
		SourceType:    coreartifact.SourceUserUpload,
		CreatedByType: coreartifact.CreatorUser,
		CreatedByID:   userID,
		Share:         WantShare(r),
	})
}

// WantShare reads the ?share=1 flag an upload uses to ask for a public link in
// the same request. Exported because the worker upload route, in another
// package, shares this contract.
func WantShare(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("share"))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// ReceiveInput is who the artifact belongs to and what produced it. The caller
// has already decided both; this package never infers provenance from a route.
type ReceiveInput struct {
	SpaceID       string
	SourceType    string
	SourceID      string
	CreatedByType string
	CreatedByID   string
	// Share asks for a public link to be created alongside the artifact and
	// returned in the response. A share failure never fails the upload: the
	// artifact is durable, and the response reports the share error instead.
	Share bool
}

// ReceiveUpload streams one multipart file into the artifact service and writes
// the response.
//
// Exported because the worker API creates artifacts too. That route
// authenticates with a run token rather than a session, but it must produce the
// same object through the same service — a second upload path is how the two
// would come to disagree about limits, naming, and failure handling.
func ReceiveUpload(w http.ResponseWriter, r *http.Request, svc *artifactsvc.Service, in ReceiveInput) {
	// The service caps the file itself; this is the backstop that stops a
	// request body from being read at all past the point where it could still
	// be accepted.
	r.Body = http.MaxBytesReader(w, r.Body, svc.MaxBytes()+uploadSlackBytes)

	// Streamed rather than parsed into a form: ParseMultipartForm spools the
	// whole upload to a temp file before the service ever sees it, which pays
	// for every byte twice and puts artifact content on the server's disk on a
	// path that has nothing to do with where it is meant to be stored.
	reader, err := r.MultipartReader()
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "expected a multipart upload")
		return
	}
	part, err := filePart(reader)
	if err != nil {
		writeUploadError(w, err, "")
		return
	}
	defer func() { _ = part.Close() }()

	rec, err := svc.Create(r.Context(), artifactsvc.CreateInput{
		SpaceID:       in.SpaceID,
		Filename:      part.FileName(),
		Title:         title(r),
		SourceType:    in.SourceType,
		SourceID:      in.SourceID,
		CreatedByType: in.CreatedByType,
		CreatedByID:   in.CreatedByID,
		Content:       part,
	})
	if err != nil {
		writeUploadError(w, err, in.SpaceID)
		return
	}
	resp := toResponse(rec)
	resp.URL = svc.PortalURL(rec.ID)
	if in.Share {
		attachShare(r, svc, rec, &resp)
	}
	httputil.WriteJSON(w, http.StatusCreated, resp)
}

// attachShare creates a public link for a just-uploaded artifact and folds it
// into the response, or records why it could not. The upload has already
// succeeded and is not undone: a share is a separate commit, so a deployment
// with sharing off, or a transient share-store failure, still returns the
// artifact — with a share_error the tool can relay rather than a missing link
// it might invent.
func attachShare(r *http.Request, svc *artifactsvc.Service, rec *coreartifact.Artifact, resp *artifactResponse) {
	share, err := svc.CreateShare(r.Context(), artifactsvc.CreateShareInput{
		ArtifactID:    rec.ID,
		CreatedByType: rec.CreatedByType,
		CreatedByID:   rec.CreatedByID,
	})
	if err != nil {
		msg, _ := apierr.Message(err)
		if msg == "" {
			msg = "could not create a public link"
		}
		resp.ShareError = msg
		return
	}
	out := toShareResponse(share.Record)
	out.Token = share.Token
	out.URL = share.PageURL
	out.DownloadURL = share.DownloadURL
	resp.Share = &out
}

var errNoFilePart = errors.New("no file part in upload")

// filePart returns the first part carrying a file, skipping any fields that
// come before it. Parts after it are not read: one artifact is one file, and
// the request is already being streamed straight into storage by then.
func filePart(reader *multipart.Reader) (*multipart.Part, error) {
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			return nil, errNoFilePart
		}
		if err != nil {
			return nil, err
		}
		if part.FormName() == uploadFormField && part.FileName() != "" {
			return part, nil
		}
		_ = part.Close()
	}
}

// title reads the label from the query string rather than a form field, so it
// does not depend on the client putting it before the file in the body — by the
// time a later part could be read, the file has already been streamed away.
func title(r *http.Request) string {
	t := strings.TrimSpace(r.URL.Query().Get("title"))
	if len(t) > maxTitleLen {
		return t[:maxTitleLen]
	}
	return t
}

func writeUploadError(w http.ResponseWriter, err error, spaceID string) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) || errors.Is(err, artifactsvc.ErrTooLarge) {
		httputil.WriteJSONError(w, http.StatusRequestEntityTooLarge, artifactsvc.ErrTooLarge.Error())
		return
	}
	if errors.Is(err, errNoFilePart) {
		httputil.WriteJSONError(w, http.StatusBadRequest, "upload must include a file part named \""+uploadFormField+"\"")
		return
	}
	if httputil.WriteServiceError(w, err) {
		return
	}
	httputil.WriteInternalError(w, err, "handler error", "handler", "upload_artifact", "space_id", spaceID)
}
