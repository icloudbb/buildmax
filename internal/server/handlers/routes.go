package handlers

import (
	"net/http"
)

// Register adds every route to one mux: auth (unauthenticated), user API (JWT),
// worker API (run token), inbound webhook. It composes RegisterPublic and
// RegisterWorker for callers that want the whole surface on a single mux —
// tests, and any single-listener embedding. The server itself does not use it:
// it registers the two sets on two listeners so the public socket cannot
// dispatch a worker route. See docs/design/worker-api-network-boundary.md.
func (h *Handler) Register(mux *http.ServeMux) {
	h.RegisterPublic(mux)
	h.RegisterWorker(mux)
}

// RegisterPublic adds every route except the worker control API. This is the
// public listener's route set: Portal, user API, webhooks, WebSocket, plugins,
// and managed inference.
func (h *Handler) RegisterPublic(mux *http.ServeMux) {
	// Establishing a session lives in its own package: those routes run before a
	// caller has one, and its Config holds no space store, so nothing there can
	// decide what a session may reach.
	h.auth.Register(mux)

	// What the acting account owns across spaces -- webhook keys today -- is
	// top-level, not space-scoped. See the route conventions in docs/contribute/architecture/server.md
	h.account.Register(mux)

	// What a space owns -- membership, agents, usage, audit trail -- lives in its
	// own package, holding exactly the stores those routes read.
	h.space.Register(mux)

	// The work surface -- issues, workflows, tasks, conversations, and the files
	// and traces their runs leave behind -- is one package because those
	// entities are one story, not four that happen to sit together.
	h.work.Register(mux)

	// Artifacts are their own object with their own authorization shape: a
	// route addressed by artifact ID takes the space from the record, not the path.
	// See docs/design/unified-artifacts.md.
	h.artifact.Register(mux)

	// The plugin catalog is deployment-scoped and readable by any active
	// account: browsing changes nothing, and a release only takes effect when
	// somebody installs it deliberately. Publishing is the administrator's
	// half and lives in the admin package.
	mux.HandleFunc("GET /api/plugins", h.listPluginsHandler)
	mux.HandleFunc("GET /api/plugins/{plugin_name}", h.getPluginHandler)
	mux.HandleFunc("GET /api/plugins/{plugin_name}/releases/{version}/download", h.downloadPluginReleaseHandler)

	// Managed LLM gateway. Not space-scoped: every catalog model is available to
	// every signed-in user, and a call is attributed to the person who made it.
	// See docs/design/client-modes.md.
	mux.HandleFunc("GET /api/llm/models", h.listLLMModelsHandler)
	mux.HandleFunc("POST /api/llm/completions", h.llmCompletionsHandler)

	// WebSocket
	mux.HandleFunc("GET /api/spaces/{space_id}/ws", h.wsUpgradeHandler)

	// System administration lives in its own package: every route there requires
	// a system_admin grant and none is space-scoped, so it holds a Config that
	// cannot reach a space's data at all.
	h.admin.Register(mux)

	// Inbound webhook
	mux.HandleFunc("POST /api/webhook", h.serveWebhook)
}

// RegisterWorker adds only the worker control API (/api/worker/*). This is the
// internal listener's route set. It lives in its own package: its routes
// authenticate with a run token rather than a user's session, and a Config that
// holds neither user store nor space store cannot be talked into honouring one.
func (h *Handler) RegisterWorker(mux *http.ServeMux) {
	h.worker.Register(mux)
}
