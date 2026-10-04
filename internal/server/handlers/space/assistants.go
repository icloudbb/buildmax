package space

import (
	"errors"
	"net/http"
	"time"

	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	assistantsvc "github.com/icloudbb/buildmax/internal/service/assistant"
)

// assistantResponse is a Space Assistant as its Space sees it: the current
// definition, whether it answers now, its bot without the token, and the
// statement of what it discloses. See docs/design/space-assistants.md.
type assistantResponse struct {
	ID               string                      `json:"id"`
	SpaceID          string                      `json:"space_id"`
	Name             string                      `json:"name"`
	Description      string                      `json:"description"`
	Instructions     string                      `json:"instructions"`
	Model            string                      `json:"model,omitempty"`
	Roster           []coreassistant.RosterEntry `json:"roster"`
	ReadableFiles    []string                    `json:"readable_files"`
	Audience         string                      `json:"audience"`
	ServiceAccountID string                      `json:"service_account_id"`
	SponsorUserID    string                      `json:"sponsor_user_id"`
	State            string                      `json:"state"`
	Availability     coreassistant.Availability  `json:"availability"`
	Revision         int                         `json:"revision"`
	Binding          *assistantBindingResponse   `json:"binding,omitempty"`
	Statement        assistantsvc.Statement      `json:"statement"`
	CreatedBy        string                      `json:"created_by"`
	CreatedAt        time.Time                   `json:"created_at"`
	UpdatedAt        time.Time                   `json:"updated_at"`
}

type assistantBindingResponse struct {
	Platform  string    `json:"platform"`
	BotHandle string    `json:"bot_handle"`
	CreatedAt time.Time `json:"created_at"`
}

// assistantDefinitionRequest is the whole definition a create or edit
// proposes. An omitted service_account_id on create makes one.
type assistantDefinitionRequest struct {
	Name             string                      `json:"name"`
	Description      string                      `json:"description"`
	Instructions     string                      `json:"instructions"`
	Model            string                      `json:"model"`
	Roster           []coreassistant.RosterEntry `json:"roster"`
	ReadableFiles    []string                    `json:"readable_files"`
	Audience         string                      `json:"audience"`
	ServiceAccountID string                      `json:"service_account_id"`
}

func (d assistantDefinitionRequest) def() coreassistant.Definition {
	return coreassistant.Definition{
		Name: d.Name, Description: d.Description, Instructions: d.Instructions, Model: d.Model,
		Roster: d.Roster, ReadableFiles: d.ReadableFiles, Audience: d.Audience, ServiceAccountID: d.ServiceAccountID,
	}
}

type updateAssistantRequest struct {
	Definition       *assistantDefinitionRequest `json:"definition,omitempty"`
	SponsorUserID    *string                     `json:"sponsor_user_id,omitempty"`
	ConfirmStatement string                      `json:"confirm_statement,omitempty"`
}

type setAssistantStateRequest struct {
	State            string `json:"state"`
	ConfirmStatement string `json:"confirm_statement,omitempty"`
}

type bindAssistantRequest struct {
	Platform string `json:"platform"`
	Token    string `json:"token"`
}

// statementRequiredResponse is the 409 for a change that needs the owner to
// confirm what it discloses: the statement to show, and its digest to send back.
type statementRequiredResponse struct {
	Error     string                 `json:"error"`
	Statement assistantsvc.Statement `json:"statement"`
}

func assistantToResponse(v assistantsvc.View) assistantResponse {
	a := v.Assistant
	out := assistantResponse{
		ID: a.ID, SpaceID: a.SpaceID, Name: a.Def.Name, Description: a.Def.Description,
		Instructions: a.Def.Instructions, Model: a.Def.Model, Roster: a.Def.Roster,
		ReadableFiles: a.Def.ReadableFiles, Audience: a.Def.Audience, ServiceAccountID: a.Def.ServiceAccountID,
		SponsorUserID: a.SponsorUserID, State: a.State, Availability: v.Availability, Revision: a.Revision,
		Statement: v.Statement, CreatedBy: a.CreatedBy, CreatedAt: a.CreatedAt, UpdatedAt: a.UpdatedAt,
	}
	if out.Roster == nil {
		out.Roster = []coreassistant.RosterEntry{}
	}
	if out.ReadableFiles == nil {
		out.ReadableFiles = []string{}
	}
	if b := v.Binding; b != nil {
		out.Binding = &assistantBindingResponse{Platform: b.Platform, BotHandle: b.BotHandle, CreatedAt: b.CreatedAt}
	}
	return out
}

// assistantPath authenticates the caller and refuses one who is not a member
// of the path's Space. manage additionally requires owner or admin.
func (h *Handler) assistantPath(w http.ResponseWriter, r *http.Request, manage bool) (userID, spaceID string, ok bool) {
	userID, ok = h.guard().UserAndStore(w, r, h.cfg.Assistants, "assistants not configured")
	if !ok {
		return "", "", false
	}
	spaceID, ok = httputil.PathValue(w, r, "space_id")
	if !ok {
		return "", "", false
	}
	if _, resolved, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolved == "" {
		return "", "", false
	}
	if manage {
		if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageAssistants); !ok {
			return "", "", false
		}
	}
	return userID, spaceID, true
}

func (h *Handler) writeAssistant(w http.ResponseWriter, status int, v *assistantsvc.View, err error, handler, userID, spaceID string) {
	if err == nil {
		httputil.WriteJSON(w, status, assistantToResponse(*v))
		return
	}
	var stmt *assistantsvc.StatementError
	if errors.As(err, &stmt) {
		httputil.WriteJSON(w, http.StatusConflict, statementRequiredResponse{Error: stmt.Error(), Statement: stmt.Statement})
		return
	}
	if httputil.WriteServiceError(w, err) {
		return
	}
	httputil.WriteInternalError(w, err, "handler error", "handler", handler, "user_id", userID, "space_id", spaceID)
}

// listAssistantsHandler serves GET /api/spaces/{space_id}/assistants.
func (h *Handler) listAssistantsHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, false)
	if !ok {
		return
	}
	list, err := h.cfg.Assistants.List(r.Context(), spaceID)
	if err != nil {
		h.writeAssistant(w, 0, nil, err, "list_assistants", userID, spaceID)
		return
	}
	out := make([]assistantResponse, len(list))
	for i := range list {
		out[i] = assistantToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// createAssistantHandler serves POST /api/spaces/{space_id}/assistants.
func (h *Handler) createAssistantHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, true)
	if !ok {
		return
	}
	var req assistantDefinitionRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	v, err := h.cfg.Assistants.Create(r.Context(), assistantsvc.CreateCmd{SpaceID: spaceID, ActorID: userID, Def: req.def()})
	h.writeAssistant(w, http.StatusCreated, v, err, "create_assistant", userID, spaceID)
}

// getAssistantHandler serves GET /api/spaces/{space_id}/assistants/{assistant_id}.
func (h *Handler) getAssistantHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, false)
	if !ok {
		return
	}
	id, ok := httputil.PathValue(w, r, "assistant_id")
	if !ok {
		return
	}
	v, err := h.cfg.Assistants.Get(r.Context(), spaceID, id)
	h.writeAssistant(w, http.StatusOK, v, err, "get_assistant", userID, spaceID)
}

// updateAssistantHandler serves PATCH /api/spaces/{space_id}/assistants/{assistant_id}.
func (h *Handler) updateAssistantHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, true)
	if !ok {
		return
	}
	id, ok := httputil.PathValue(w, r, "assistant_id")
	if !ok {
		return
	}
	var req updateAssistantRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	cmd := assistantsvc.UpdateCmd{
		SpaceID: spaceID, ActorID: userID, AssistantID: id,
		SponsorUserID: req.SponsorUserID, ConfirmStatement: req.ConfirmStatement,
	}
	if req.Definition != nil {
		def := req.Definition.def()
		cmd.Def = &def
	}
	v, err := h.cfg.Assistants.Update(r.Context(), cmd)
	h.writeAssistant(w, http.StatusOK, v, err, "update_assistant", userID, spaceID)
}

// setAssistantStateHandler serves PUT /api/spaces/{space_id}/assistants/{assistant_id}/state.
func (h *Handler) setAssistantStateHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, true)
	if !ok {
		return
	}
	id, ok := httputil.PathValue(w, r, "assistant_id")
	if !ok {
		return
	}
	var req setAssistantStateRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	v, err := h.cfg.Assistants.SetState(r.Context(), assistantsvc.SetStateCmd{
		SpaceID: spaceID, ActorID: userID, AssistantID: id, State: req.State, ConfirmStatement: req.ConfirmStatement,
	})
	h.writeAssistant(w, http.StatusOK, v, err, "set_assistant_state", userID, spaceID)
}

// deleteAssistantHandler serves DELETE /api/spaces/{space_id}/assistants/{assistant_id}.
func (h *Handler) deleteAssistantHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, true)
	if !ok {
		return
	}
	id, ok := httputil.PathValue(w, r, "assistant_id")
	if !ok {
		return
	}
	if err := h.cfg.Assistants.Delete(r.Context(), spaceID, userID, id); err != nil {
		h.writeAssistant(w, 0, nil, err, "delete_assistant", userID, spaceID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bindAssistantHandler serves PUT /api/spaces/{space_id}/assistants/{assistant_id}/binding.
// The token is checked with the platform, sealed, and never returned.
func (h *Handler) bindAssistantHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, true)
	if !ok {
		return
	}
	id, ok := httputil.PathValue(w, r, "assistant_id")
	if !ok {
		return
	}
	var req bindAssistantRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	v, err := h.cfg.Assistants.Bind(r.Context(), assistantsvc.BindCmd{
		SpaceID: spaceID, ActorID: userID, AssistantID: id, Platform: req.Platform, Token: req.Token,
	})
	h.writeAssistant(w, http.StatusOK, v, err, "bind_assistant", userID, spaceID)
}

// unbindAssistantHandler serves DELETE /api/spaces/{space_id}/assistants/{assistant_id}/binding.
func (h *Handler) unbindAssistantHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.assistantPath(w, r, true)
	if !ok {
		return
	}
	id, ok := httputil.PathValue(w, r, "assistant_id")
	if !ok {
		return
	}
	v, err := h.cfg.Assistants.Unbind(r.Context(), spaceID, userID, id)
	h.writeAssistant(w, http.StatusOK, v, err, "unbind_assistant", userID, spaceID)
}
