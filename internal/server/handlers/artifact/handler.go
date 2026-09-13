// Package artifact serves the durable files a space keeps.
//
// It is its own package because an artifact is its own object, and because its
// authorization is a different shape from every other route here: an artifact
// is addressed by its opaque ID, and the space comes from the record rather than
// from the path. Folding it into the work surface would put that different
// shape next to routes that all take their space from the URL, which is the
// mistake most likely to be copied. See docs/design/unified-artifacts.md.
package artifact

import (
	"net/http"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/server/access"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

type Config struct {
	JWTSecret string

	Users    coreidentity.UserStore
	Spaces   corespace.Store
	Sessions coreidentity.AuthSessionStore

	// Artifacts is the capability itself. Nil means this deployment has no
	// artifact store, and every route here answers 503.
	Artifacts *artifactsvc.Service
	Audit     *audit.Recorder
}

type Handler struct{ cfg Config }

func New(cfg Config) *Handler { return &Handler{cfg: cfg} }

func (h *Handler) guard() *access.Guard {
	return &access.Guard{
		JWTSecret: h.cfg.JWTSecret,
		Users:     h.cfg.Users,
		Spaces:    h.cfg.Spaces,
		Sessions:  h.cfg.Sessions,
		Audit:     h.cfg.Audit,
	}
}

// Register adds the artifact routes.
//
// The split is deliberate: an artifact is reached by its ID, and the
// space-scoped route is the space's listing and upload surface, not a second
// address for one artifact.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/spaces/{space_id}/artifacts", h.listArtifactsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/artifacts", h.uploadArtifactHandler)
	// The same creation, for a client that has a login but has not chosen a
	// space: an optional ?space_id= is honoured, and no space means the caller's
	// personal one. CLI and Desktop reach artifacts this way.
	mux.HandleFunc("POST /api/artifacts", h.uploadToDefaultSpaceHandler)
	mux.HandleFunc("GET /api/artifacts/{artifact_id}", h.getArtifactHandler)
	mux.HandleFunc("GET /api/artifacts/{artifact_id}/content", h.artifactContentHandler)
	mux.HandleFunc("DELETE /api/artifacts/{artifact_id}", h.deleteArtifactHandler)

	// Share management is an authenticated app action on one artifact, so it
	// keeps the /api prefix and the same space authorization as the routes above.
	mux.HandleFunc("POST /api/artifacts/{artifact_id}/shares", h.createShareHandler)
	mux.HandleFunc("GET /api/artifacts/{artifact_id}/shares", h.listSharesHandler)
	mux.HandleFunc("DELETE /api/artifacts/{artifact_id}/shares/{share_id}", h.revokeShareHandler)

	// The public share surface is sessionless and token-only, so it wears a
	// friendly top-level namespace rather than /api. The bare
	// /shared/artifacts/{token} page is the Portal SPA's; only these two machine
	// leaves are the backend's. The namespace is /shared/<resource>/ so a future
	// shared issue or conversation is a sibling rather than a special case. See
	// docs/design/artifact-public-sharing-and-preview.md §6.
	mux.HandleFunc("GET /shared/artifacts/{token}/meta", h.sharedMetaHandler)
	mux.HandleFunc("GET /shared/artifacts/{token}/raw", h.sharedContentHandler)
}
