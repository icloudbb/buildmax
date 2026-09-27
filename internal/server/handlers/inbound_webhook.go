package handlers

import (
	"errors"
	"io"
	"net/http"
	"strings"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/access"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
)

func (h *Handler) serveWebhook(w http.ResponseWriter, r *http.Request) {
	if h.cfg.WebhookEngine == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "webhook not configured")
		return
	}
	key := h.extractWebhookKey(r)
	if key == "" {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "missing webhook key (Authorization: Bearer <key> or X-Webhook-Key)")
		return
	}
	if h.cfg.UserWebhookKeyStore == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "webhook keys not configured")
		return
	}
	resolvedUserID, err := h.cfg.UserWebhookKeyStore.GetUserIDByKey(r.Context(), key)
	if err != nil {
		httputil.WriteInternalError(w, err, "webhook handler", "handler", "get_user_by_key")
		return
	}
	if resolvedUserID == "" {
		httputil.WriteJSONError(w, http.StatusUnauthorized, "invalid webhook key")
		return
	}
	// A webhook key is a credential the account holds, so disabling the account
	// refuses it too. Nothing revokes the key itself: enabling the account
	// should bring the integration back without the operator having to reissue
	// keys to whatever is calling this.
	if h.cfg.UserStore != nil {
		owner, err := h.cfg.UserStore.GetUser(r.Context(), resolvedUserID)
		if err != nil {
			httputil.WriteInternalError(w, err, "webhook handler", "handler", "webhook_owner", "user_id", resolvedUserID)
			return
		}
		if owner != nil && owner.Disabled() {
			httputil.WriteJSONError(w, http.StatusForbidden, access.DisabledMessage)
			return
		}
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.WriteInternalError(w, err, "webhook handler", "handler", "read_body")
		return
	}
	req := &convchannel.WebhookRequest{
		Body:   body,
		Header: map[string][]string(r.Header.Clone()),
	}
	turn, err := h.cfg.WebhookAdapter.Receive(r.Context(), req)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	turn.UserID = resolvedUserID
	if h.cfg.ConversationStore == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "conversations not configured")
		return
	}
	conv, err := h.cfg.ConversationStore.CreateConversation(r.Context(), resolvedUserID, convchannel.ChannelWebhook, resolvedUserID)
	if err != nil {
		httputil.WriteInternalError(w, err, "webhook handler", "handler", "create_conversation")
		return
	}
	result, err := h.cfg.WebhookEngine.Process(r.Context(), conv.ID, "", turn)
	if err != nil {
		if errors.Is(err, coretask.ErrRunInProgress) {
			httputil.WriteJSONError(w, http.StatusConflict, "task has a run already in progress")
			return
		}
		httputil.WriteInternalError(w, err, "webhook handler", "handler", "process")
		return
	}
	if len(result.Runs) == 0 {
		httputil.WriteInternalError(w, nil, "webhook handler", "handler", "no_run")
		return
	}
	spawned := result.Runs[0]
	httputil.WriteJSON(w, http.StatusAccepted, map[string]string{
		"task_id":     spawned.TaskID,
		"task_run_id": spawned.RunID,
	})
}

func (h *Handler) extractWebhookKey(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
	}
	if k := r.Header.Get("X-Webhook-Key"); k != "" {
		return strings.TrimSpace(k)
	}
	return ""
}
