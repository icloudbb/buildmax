package artifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
)

// ErrSharingNotConfigured is a deployment that cannot make a public link: it
// has no share store, or no public base URL to render one against. It is a
// not-configured kind so a caller is told the capability is off rather than
// that their request was bad.
var ErrSharingNotConfigured = apierr.New(apierr.KindNotConfigured, "public sharing is not configured")

// DefaultShareTTL bounds a link's life when a deployment sets none. Matched to
// the refresh-token horizon: long enough to hand a document to someone and have
// the link still work a month later, short enough that a forgotten link lapses.
const DefaultShareTTL = 30 * 24 * time.Hour

// sharePrefix marks the token so a leaked one is greppable and secret scanning
// can recognise it, the same idea as the webhook key's whsec_ prefix.
const sharePrefix = "share_"
const shareTokenBytes = 32

// randRead is a seam so a test can make token generation deterministic.
var randRead = rand.Read

// SharesAvailable reports whether this deployment can create public links.
func (s *Service) SharesAvailable() bool {
	return s.Available() && s.Shares != nil && strings.TrimSpace(s.PublicBaseURL) != ""
}

func (s *Service) shareTTL() time.Duration {
	if s != nil && s.ShareTTL > 0 {
		return s.ShareTTL
	}
	return DefaultShareTTL
}

// CreateShareInput asks for a link to one artifact.
type CreateShareInput struct {
	ArtifactID string
	// TTL caps how long the link lives. Zero uses the deployment default; a
	// value above the deployment bound is clamped down to it, never up.
	TTL time.Duration
	// CreatedByType/ID attribute the link. A worker-created share records the
	// initiating user, so the link is attributable even though it is opened
	// anonymously.
	CreatedByType string
	CreatedByID   string
}

// Share is a created link and the URLs a caller cites. The token is present
// only here, once: it is never returned by any read.
type Share struct {
	Record      coreartifact.ArtifactShare
	Token       string
	PageURL     string
	DownloadURL string
}

// CreateShare mints a public link for a live artifact.
func (s *Service) CreateShare(ctx context.Context, in CreateShareInput) (*Share, error) {
	if !s.SharesAvailable() {
		return nil, ErrSharingNotConfigured
	}
	// The artifact must be live and readable: Get refuses a tombstoned one, so a
	// link is never created for content that is already gone.
	art, err := s.Get(ctx, in.ArtifactID)
	if err != nil {
		return nil, err
	}

	token, hash, err := newShareToken()
	if err != nil {
		return nil, err
	}
	expires := s.now().UTC().Add(clampTTL(in.TTL, s.shareTTL()))
	rec, err := s.Shares.CreateArtifactShare(ctx, coreartifact.CreateShareInput{
		ArtifactID:    art.ID,
		SpaceID:       art.SpaceID,
		TokenSHA256:   hash,
		CreatedByType: in.CreatedByType,
		CreatedByID:   in.CreatedByID,
		ExpiresAt:     &expires,
	})
	if err != nil {
		return nil, err
	}
	s.auditShare(ctx, art.SpaceID, rec, in.CreatedByType, in.CreatedByID, coreaudit.ArtifactShareCreated)
	return &Share{
		Record:      *rec,
		Token:       token,
		PageURL:     s.sharePageURL(token),
		DownloadURL: s.shareDownloadURL(token),
	}, nil
}

// ResolveShare turns a public token into the artifact behind it, or ErrNotFound
// for anything a public caller must not tell apart: an unknown token, a revoked
// or expired link, or a tombstoned artifact.
func (s *Service) ResolveShare(ctx context.Context, token string) (*coreartifact.ResolvedShare, error) {
	if !s.Available() || s.Shares == nil {
		return nil, ErrNotFound
	}
	resolved, err := s.Shares.GetArtifactShareByTokenHash(ctx, hashShareToken(token))
	if err != nil {
		return nil, err
	}
	if resolved == nil || !resolved.Share.Live(s.now()) || resolved.Artifact.Deleted() {
		return nil, ErrNotFound
	}
	return resolved, nil
}

// RecordShareRetrieval counts one content fetch. Best-effort: it never fails a
// download, because the counter is telemetry and the bytes are the point.
func (s *Service) RecordShareRetrieval(ctx context.Context, shareID string) {
	if s.Shares == nil {
		return
	}
	_ = s.Shares.RecordArtifactShareRetrieval(ctx, shareID, s.now().UTC())
}

// ListShares returns an artifact's links for the management view.
func (s *Service) ListShares(ctx context.Context, artifactID string) ([]coreartifact.ArtifactShare, error) {
	if !s.Available() || s.Shares == nil {
		return nil, ErrSharingNotConfigured
	}
	return s.Shares.ListArtifactShares(ctx, artifactID)
}

// RevokeShare withdraws a link on an artifact the caller has already resolved.
// A repeat revoke, or a share that never belonged to this artifact, reports
// ErrNotFound and writes no audit event.
func (s *Service) RevokeShare(ctx context.Context, rec *coreartifact.Artifact, shareID, actorType, actorID string) error {
	if !s.Available() || s.Shares == nil {
		return ErrSharingNotConfigured
	}
	if rec == nil {
		return ErrNotFound
	}
	changed, err := s.Shares.RevokeArtifactShare(ctx, rec.ID, shareID, s.now().UTC())
	if err != nil {
		return err
	}
	if !changed {
		return ErrNotFound
	}
	if s.Audit != nil {
		s.Audit.Record(ctx, coreaudit.Event{
			SpaceID:    rec.SpaceID,
			ActorType:  auditActorFor(actorType),
			ActorID:    actorID,
			Action:     coreaudit.ArtifactShareRevoked,
			TargetType: "artifact",
			TargetID:   rec.ID,
			Detail:     shareID,
		})
	}
	return nil
}

// PortalURL is where a space member opens the artifact in the Portal, or ""
// when the deployment has no public origin. It is the only address an agent
// is given for an artifact: the address a worker reaches the server at is
// internal and opens nothing for a person.
func (s *Service) PortalURL(artifactID string) string {
	if s == nil || strings.TrimSpace(s.PublicBaseURL) == "" || artifactID == "" {
		return ""
	}
	return strings.TrimRight(s.PublicBaseURL, "/") + "/#/artifact/" + url.PathEscape(artifactID)
}

func (s *Service) sharePageURL(token string) string {
	return strings.TrimRight(s.PublicBaseURL, "/") + "/shared/artifacts/" + token
}

func (s *Service) shareDownloadURL(token string) string {
	return s.sharePageURL(token) + "/raw?dl=1"
}

// auditShare records a share event on the artifact target, with the share ID as
// detail. The token is deliberately absent.
func (s *Service) auditShare(ctx context.Context, spaceID string, rec *coreartifact.ArtifactShare, actorType, actorID, action string) {
	if s.Audit == nil || rec == nil {
		return
	}
	s.Audit.Record(ctx, coreaudit.Event{
		SpaceID:    spaceID,
		ActorType:  auditActorFor(actorType),
		ActorID:    actorID,
		Action:     action,
		TargetType: "artifact",
		TargetID:   rec.ArtifactID,
		Detail:     rec.ShareID,
	})
}

// clampTTL keeps a requested lifetime within the deployment bound: zero takes
// the bound, and a longer request is brought down to it, never up.
func clampTTL(requested, bound time.Duration) time.Duration {
	if requested <= 0 || requested > bound {
		return bound
	}
	return requested
}

func newShareToken() (token, hash string, err error) {
	b := make([]byte, shareTokenBytes)
	if _, err := randRead(b); err != nil {
		return "", "", err
	}
	token = sharePrefix + hex.EncodeToString(b)
	return token, hashShareToken(token), nil
}

func hashShareToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
