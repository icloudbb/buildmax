package handlers

import (
	"encoding/json"
	"github.com/icloudbb/buildmax/internal/server/handlers/llmhttp"
	"net/http"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// The managed inference wire contract lives in internal/infra/llmwire, so the
// handler and the remote client marshal one definition rather than two that can
// drift. See docs/design/llm-gateway.md section 8.

// listLLMModelsHandler serves GET /api/llm/models.
func (h *Handler) listLLMModelsHandler(w http.ResponseWriter, r *http.Request) {
	// Being signed in is the whole authorization: every catalog model is
	// available to every user of the deployment. Identity is still resolved
	// before the gateway is touched, so an unauthenticated caller learns nothing
	// about whether this deployment offers managed inference.
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return
	}
	if !llmhttp.RequireGateway(w, h.cfg.LLMGateway) {
		return
	}
	models, err := h.cfg.LLMGateway.Models(r.Context())
	if err != nil {
		llmhttp.WriteError(w, err, "llm_models", userID)
		return
	}
	out := make([]llmwire.Model, 0, len(models))
	for _, m := range models {
		capabilities := make([]string, 0, len(m.Capabilities))
		for _, c := range m.Capabilities {
			capabilities = append(capabilities, string(c))
		}
		out = append(out, llmwire.Model{
			Name:          m.Name,
			ContextWindow: m.ContextWindow,
			Vision:        m.Vision,
			Capabilities:  capabilities,
			Default:       m.Default,
			Pricing:       wirePricing(m.Pricing),
		})
	}
	httputil.WriteJSON(w, http.StatusOK, llmwire.ModelsResponse{Models: out})
}

func wirePricing(p cllm.Pricing) *llmwire.ModelPricing {
	if p.Currency == "" {
		return nil
	}
	return &llmwire.ModelPricing{
		Currency:          p.Currency,
		InputPerMTok:      cllm.FormatRate(p.InputPerMTok),
		CacheReadPerMTok:  cllm.FormatRate(p.CacheReadPerMTok),
		CacheWritePerMTok: cllm.FormatRate(p.CacheWritePerMTok),
		OutputPerMTok:     cllm.FormatRate(p.OutputPerMTok),
	}
}

// llmCompletionsHandler serves POST /api/llm/completions.
func (h *Handler) llmCompletionsHandler(w http.ResponseWriter, r *http.Request) {
	// Authorize before the gateway check; see listLLMModelsHandler.
	userID, ok := h.guard().ActiveUser(w, r)
	if !ok {
		return
	}
	if !llmhttp.RequireGateway(w, h.cfg.LLMGateway) {
		return
	}

	// Unknown fields are rejected rather than ignored: a client that thinks it
	// set a generation parameter must not be told silently that it did.
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req llmwire.CompletionRequest
	if err := decoder.Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	messages, err := llmhttp.CoreMessages(req.Messages)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	profile, err := llmhttp.CallProfile(req.CallProfile)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	// No SpaceID: a foreground call is somebody's own work and is metered against
	// no space. See docs/design/client-modes.md section 9.
	cmd := llmgateway.CompleteRequest{
		UserID:       &userID,
		ClientCallID: req.CallID,
		Model:        req.Model,
		Messages:     messages,
		Tools:        llmhttp.CoreTools(req.Tools),
		CallProfile:  profile,
	}
	if req.Metadata != nil {
		cmd.Surface = req.Metadata.Surface
		cmd.SessionID = req.Metadata.SessionID
	}

	if req.Stream {
		llmhttp.Stream(w, r, h.cfg.LLMGateway, cmd, userID)
		return
	}

	result, err := h.cfg.LLMGateway.Complete(r.Context(), cmd)
	if err != nil {
		llmhttp.WriteError(w, err, "llm_completions", userID)
		return
	}

	resp := llmwire.CompletionResponse{
		LLMCallID:     result.LLMCallID,
		Model:         result.Model,
		Content:       result.Content,
		ToolCalls:     llmhttp.WireToolCalls(result.ToolCalls),
		ProviderState: llmhttp.WireProviderState(result.ProviderState),
	}
	if result.UsageReported {
		resp.Usage = &llmwire.Usage{
			PromptTokens:     result.Usage.PromptTokens,
			CompletionTokens: result.Usage.CompletionTokens,
			TotalTokens:      result.Usage.TotalTokens,
			CacheReadTokens:  result.Usage.CacheReadTokens,
			CacheWriteTokens: result.Usage.CacheWriteTokens,
		}
	}
	httputil.WriteJSON(w, http.StatusOK, resp)
}

// streamLLMCompletion serves a streaming managed call over SSE.
//

// sseWriter emits typed server-sent events, flushing each one.
//
