package account

import (
	"encoding/json"
	"net/http"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

type createWebhookKeyRequest struct {
	Name string `json:"name"`
}

type createWebhookKeyResponse struct {
	Key string `json:"key"`
	ID  string `json:"id"`
}

type listWebhookKeysResponse struct {
	Keys []webhookKeyMetaResponse `json:"keys"`
}

type webhookKeyMetaResponse struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (h *Handler) createWebhookKeyHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.WebhookKeys, "webhook keys not configured")
	if !ok {
		return
	}
	var req createWebhookKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	plaintextKey, keyID, err := h.cfg.WebhookKeys.CreateKey(r.Context(), userID, req.Name)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_webhook_key", "user_id", userID)
		return
	}
	// A webhook key is account-scoped, so the event carries no space.
	h.cfg.Audit.UserAction(r.Context(), userID, "", coreaudit.WebhookKeyCreated, "webhook_key", keyID, "")
	httputil.WriteJSON(w, http.StatusCreated, createWebhookKeyResponse{Key: plaintextKey, ID: keyID})
}

func (h *Handler) listWebhookKeysHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.WebhookKeys, "webhook keys not configured")
	if !ok {
		return
	}
	list, err := h.cfg.WebhookKeys.ListKeys(r.Context(), userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_webhook_keys", "user_id", userID)
		return
	}
	out := make([]webhookKeyMetaResponse, len(list))
	for i := range list {
		out[i] = webhookKeyMetaResponse{ID: list[i].KeyID, Name: list[i].Name, CreatedAt: list[i].CreatedAt}
	}
	httputil.WriteJSON(w, http.StatusOK, listWebhookKeysResponse{Keys: out})
}

func (h *Handler) revokeWebhookKeyHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.WebhookKeys, "webhook keys not configured")
	if !ok {
		return
	}
	keyID := r.PathValue("key_id")
	if keyID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "key_id required")
		return
	}
	err := h.cfg.WebhookKeys.RevokeKey(r.Context(), userID, keyID)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "webhook key not found")
		return
	}
	h.cfg.Audit.UserAction(r.Context(), userID, "", coreaudit.WebhookKeyRevoked, "webhook_key", keyID, "")
	w.WriteHeader(http.StatusNoContent)
}
