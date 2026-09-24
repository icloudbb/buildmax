package admin

import (
	"encoding/json"
	"net/http"
	"strings"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/llmcatalog"
)

// AdminModel is one catalog entry as an administrator sees it.
//
// coregw.Model carries no credential by construction — the key lives in the
// same table but leaves the store only through LLMModelCredential — so this
// embeds it rather than copying field by field.
//
// Nothing is added: every enabled model is callable by every user, so a row's
// name and enabled state are the whole answer to "can this be used".
type AdminModel struct {
	coregw.Model
}

// AdminModelsResponse is the managed catalog.
type AdminModelsResponse struct {
	Models []AdminModel `json:"models"`
	// DefaultModel is the model name a caller gets when it names none. Empty
	// means llm.default_model was not configured and the first enabled model
	// serves as the default.
	DefaultModel string `json:"default_model,omitempty"`
}

// listAdminModelsHandler serves GET /api/admin/llm/models.
func (h *Handler) listAdminModelsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Models, "the model catalog is not configured") {
		return
	}
	models, err := h.cfg.Models.ListLLMModels(r.Context())
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_models")
		return
	}
	out := make([]AdminModel, 0, len(models))
	for _, m := range models {
		out = append(out, AdminModel{Model: m})
	}
	httputil.WriteJSON(w, http.StatusOK, AdminModelsResponse{
		Models:       out,
		DefaultModel: h.cfg.Deployment.DefaultModel,
	})
}

// AdminCreateModelRequest is the body of POST /api/admin/llm/models. It mirrors
// the fields `buildmax-server model add` takes so a model added through either
// edge lands as the same row.
//
// api_key is write-only: it is accepted here in the body (never a query or path
// parameter, which the request log would record), stored encrypted at rest, and
// returned by no read — the response carries coregw.Model, which has no
// credential field. Prices are strings in the model's currency, resolved the
// same way the shell command resolves its flags.
type AdminCreateModelRequest struct {
	Name            string   `json:"name"`
	ProviderType    string   `json:"provider_type"`
	APIURL          string   `json:"api_url"`
	APIKey          string   `json:"api_key"`
	Model           string   `json:"model"`
	ContextWindow   int      `json:"context_window"`
	CallTimeout     int      `json:"call_timeout"`
	MaxTokens       int      `json:"max_tokens"`
	Reasoning       string   `json:"reasoning"`
	CacheMode       string   `json:"cache_mode"`
	CacheTTL        string   `json:"cache_ttl"`
	Currency        string   `json:"currency"`
	InputPrice      string   `json:"input_price"`
	CacheReadPrice  string   `json:"cache_read_price"`
	CacheWritePrice string   `json:"cache_write_price"`
	OutputPrice     string   `json:"output_price"`
	Vision          bool     `json:"vision"`
	Capabilities    []string `json:"capabilities"`
}

// createAdminModelHandler serves POST /api/admin/llm/models.
//
// Adding a model over HTTP is safe now that the credential is encrypted at rest
// (see internal/infra/secret and the store): a deployment with an encryption key
// accepts one here, and one without refuses it — the same rule the shell command
// follows, since both reach the same catalog service.
func (h *Handler) createAdminModelHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Models, "the model catalog is not configured") {
		return
	}
	var req AdminCreateModelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pricing, err := llmcatalog.ResolvePricing(req.Currency, req.InputPrice, req.CacheReadPrice, req.CacheWritePrice, req.OutputPrice)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	in := coregw.CreateModelInput{
		Name:              strings.TrimSpace(req.Name),
		ProviderType:      strings.TrimSpace(req.ProviderType),
		APIURL:            strings.TrimSpace(req.APIURL),
		APIKey:            strings.TrimSpace(req.APIKey),
		Model:             strings.TrimSpace(req.Model),
		ContextWindow:     req.ContextWindow,
		CallTimeout:       req.CallTimeout,
		MaxTokens:         req.MaxTokens,
		Reasoning:         strings.TrimSpace(req.Reasoning),
		CacheMode:         strings.TrimSpace(req.CacheMode),
		CacheTTL:          strings.TrimSpace(req.CacheTTL),
		Currency:          pricing.Currency,
		InputPerMTok:      pricing.InputPerMTok,
		CacheReadPerMTok:  pricing.CacheReadPerMTok,
		CacheWritePerMTok: pricing.CacheWritePerMTok,
		OutputPerMTok:     pricing.OutputPerMTok,
		Vision:            req.Vision,
		Capabilities:      req.Capabilities,
	}
	svc := &llmcatalog.Service{Models: h.cfg.Models, Audit: h.cfg.Audit}
	created, err := svc.Create(r.Context(), in, coreaudit.UserActor(actorID))
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_create_model")
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, AdminModel{Model: *created})
}

// setAdminModelEnabledHandler serves the enable and disable routes.
//
// What a change records is service/llmcatalog's; this decides only that the
// caller is a person and says which one.
// setModelStateRequest is the body of PUT /api/admin/llm/models/{model_id}/state.
type setModelStateRequest struct {
	Enabled bool `json:"enabled"`
}

// setAdminModelStateHandler sets the catalog model's stored `enabled` flag. See
// the route conventions in docs/contribute/architecture/server.md
func (h *Handler) setAdminModelStateHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Models, "the model catalog is not configured") {
		return
	}
	modelID, ok := httputil.PathValue(w, r, "model_id")
	if !ok {
		return
	}
	var req setModelStateRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	svc := &llmcatalog.Service{Models: h.cfg.Models, Audit: h.cfg.Audit}
	updated, err := svc.SetEnabled(r.Context(), modelID, req.Enabled, coreaudit.UserActor(actorID))
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_set_model_enabled", "model_id", modelID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, AdminModel{Model: *updated})
}

// setModelCredentialRequest is the body of
// PUT /api/admin/llm/models/{model_id}/credential. api_key is write-only, in the
// body for the same reason as on create.
type setModelCredentialRequest struct {
	APIKey string `json:"api_key"`
}

// setAdminModelCredentialHandler replaces a catalog model's upstream key in
// place, so rotating a leaked or expired key does not rename the model every
// client selects it by.
func (h *Handler) setAdminModelCredentialHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Models, "the model catalog is not configured") {
		return
	}
	modelID, ok := httputil.PathValue(w, r, "model_id")
	if !ok {
		return
	}
	var req setModelCredentialRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	svc := &llmcatalog.Service{Models: h.cfg.Models, Audit: h.cfg.Audit}
	updated, err := svc.ReplaceCredential(r.Context(), modelID, req.APIKey, coreaudit.UserActor(actorID))
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_set_model_credential", "model_id", modelID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, AdminModel{Model: *updated})
}
