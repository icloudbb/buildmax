package artifact

import (
	"errors"
	"net/http"
	"time"

	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
)

const notFoundMessage = "artifact not found"

// artifactResponse is one artifact as the API presents it.
//
// Built by hand rather than serialized from the model so that adding a column
// cannot publish it by accident — the storage key in particular must never
// leave the server.
type artifactResponse struct {
	ID            string `json:"id"`
	SpaceID       string `json:"space_id"`
	Filename      string `json:"filename"`
	MediaType     string `json:"media_type"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	Title         string `json:"title,omitempty"`
	CreatedByType string `json:"created_by_type"`
	CreatedByID   string `json:"created_by_id,omitempty"`
	SourceType    string `json:"source_type"`
	SourceID      string `json:"source_id,omitempty"`
	// Preview reports how this deployment will show the content: "inline" to
	// render it directly, "sandbox" for an active document (HTML) that runs only
	// in an opaque-origin frame, or "none" for download-only. The client uses it
	// to choose a renderer instead of guessing from the media type itself.
	Preview   string     `json:"preview"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	// URL is the artifact's Portal page, set on an upload when the deployment
	// has a public origin. The server renders it so no client derives an
	// address from however it happens to reach the server.
	URL string `json:"url,omitempty"`
	// Share is present only on an upload that asked for a public link and got
	// one; it is the sole place the link's token is returned. ShareError is set
	// instead when a link was asked for but could not be made — the artifact is
	// still here, so the caller reports the error rather than a missing link.
	Share      *shareResponse `json:"share,omitempty"`
	ShareError string         `json:"share_error,omitempty"`
}

type artifactListResponse struct {
	Items []artifactResponse `json:"items"`
	Total int                `json:"total"`
}

// toResponse builds the wire form by hand so model fields cannot become API
// fields merely by being added to the stored entity.
func toResponse(a *coreartifact.Artifact) artifactResponse {
	out := artifactResponse{
		ID:            a.ID,
		SpaceID:       a.SpaceID,
		Filename:      a.Filename,
		MediaType:     a.MediaType,
		SizeBytes:     a.SizeBytes,
		SHA256:        a.SHA256,
		Title:         a.Title,
		CreatedByType: a.CreatedByType,
		CreatedByID:   a.CreatedByID,
		SourceType:    a.SourceType,
		SourceID:      a.SourceID,
		Preview:       string(previewModeFor(a.MediaType)),
		CreatedAt:     a.CreatedAt,
	}
	out.ExpiresAt = a.ExpiresAt
	return out
}

// service reports the capability, refusing when the deployment has none.
//
// It is never the first check on a route. Answering an unauthenticated caller
// with "artifacts not configured" would tell them something about the
// deployment before they have proved anything about themselves.
func (h *Handler) service(w http.ResponseWriter) (*artifactsvc.Service, bool) {
	if h.cfg.Artifacts == nil || !h.cfg.Artifacts.Available() {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "artifacts not configured")
		return nil, false
	}
	return h.cfg.Artifacts, true
}

// spaceCaller is the preamble of the two space-scoped routes: the caller is
// active and in the space the path names, and only then is the capability
// reported.
func (h *Handler) spaceCaller(w http.ResponseWriter, r *http.Request) (userID, spaceID string, svc *artifactsvc.Service, ok bool) {
	userID, spaceID, ok = h.guard().UserAndPathSpace(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return "", "", nil, false
	}
	svc, ok = h.service(w)
	if !ok {
		return "", "", nil, false
	}
	return userID, spaceID, svc, true
}

// resolve finds the artifact an ID names and authorizes the caller against the
// space the record says owns it.
//
// Absent, tombstoned, and not-yours are answered identically on purpose: the
// three are the same fact to anyone who should not have it.
func (h *Handler) resolve(w http.ResponseWriter, r *http.Request) (*coreartifact.Artifact, string, bool) {
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return nil, "", false
	}
	svc, ok := h.service(w)
	if !ok {
		return nil, "", false
	}
	artifactID, ok := httputil.PathValue(w, r, "artifact_id")
	if !ok {
		return nil, "", false
	}
	rec, err := svc.Get(r.Context(), artifactID)
	if err != nil {
		if errors.Is(err, artifactsvc.ErrNotFound) {
			httputil.WriteJSONError(w, http.StatusNotFound, notFoundMessage)
			return nil, "", false
		}
		if httputil.WriteServiceError(w, err) {
			return nil, "", false
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "artifact", "artifact_id", artifactID)
		return nil, "", false
	}
	if !h.guard().MemberOfResourceSpace(w, r, userID, rec.SpaceID, notFoundMessage) {
		return nil, "", false
	}
	return rec, userID, true
}

func (h *Handler) listArtifactsHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, svc, ok := h.spaceCaller(w, r)
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", 50, 200)
	items, total, err := svc.List(r.Context(), spaceID, limit, offset)
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_artifacts", "space_id", spaceID)
		return
	}
	out := make([]artifactResponse, len(items))
	for i := range items {
		out[i] = toResponse(&items[i])
	}
	httputil.WriteJSON(w, http.StatusOK, artifactListResponse{Items: out, Total: total})
}

func (h *Handler) getArtifactHandler(w http.ResponseWriter, r *http.Request) {
	rec, _, ok := h.resolve(w, r)
	if !ok {
		return
	}
	httputil.WriteJSON(w, http.StatusOK, toResponse(rec))
}

func (h *Handler) deleteArtifactHandler(w http.ResponseWriter, r *http.Request) {
	rec, userID, ok := h.resolve(w, r)
	if !ok {
		return
	}
	svc, ok := h.service(w)
	if !ok {
		return
	}
	role, ok := h.guard().SpaceRole(w, r, userID, rec.SpaceID)
	if !ok {
		return
	}
	if !mayDelete(role, userID, rec) {
		httputil.WriteJSONError(w, http.StatusForbidden, "not allowed to delete this artifact")
		return
	}
	if err := svc.Delete(r.Context(), rec, coreartifact.CreatorUser, userID); err != nil {
		if errors.Is(err, artifactsvc.ErrNotFound) {
			httputil.WriteJSONError(w, http.StatusNotFound, notFoundMessage)
			return
		}
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "delete_artifact", "artifact_id", rec.ID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mayDelete implements the first-slice policy: anyone may remove what they
// uploaded themselves, and an admin or owner may remove anything the space
// holds. A member cannot delete a colleague's file, and cannot delete what a
// run produced, because neither is theirs to withdraw.
func mayDelete(role, userID string, rec *coreartifact.Artifact) bool {
	if role == corespace.RoleAdmin || role == corespace.RoleOwner {
		return true
	}
	return rec.CreatedByType == coreartifact.CreatorUser && rec.CreatedByID == userID && userID != ""
}
