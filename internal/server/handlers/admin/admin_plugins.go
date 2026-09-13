package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	archive "github.com/icloudbb/buildmax/internal/infra/pluginarchive"
	"github.com/icloudbb/buildmax/internal/infra/pluginwire"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
)

// The catalog as an administrator sees it includes archived entries and yanked
// releases: hiding a retired entry from the person who retired it would leave
// no way to restore it.

// listAdminPluginsHandler serves GET /api/admin/plugins.
func (h *Handler) listAdminPluginsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !h.requirePlugins(w) {
		return
	}
	plugins, err := h.cfg.Plugins.ListEntries(r.Context(), true)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_plugins")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, pluginwire.CatalogResponse{Plugins: plugins})
}

// listAdminPluginReleasesHandler serves GET /api/admin/plugins/{plugin_name}/releases.
func (h *Handler) listAdminPluginReleasesHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !h.requirePlugins(w) {
		return
	}
	name, ok := httputil.PathValue(w, r, "plugin_name")
	if !ok {
		return
	}
	releases, err := h.cfg.Plugins.ListReleases(r.Context(), name)
	if err != nil {
		writePluginError(w, err, "admin_list_plugin_releases", name)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, pluginwire.ReleasesResponse{Releases: releases})
}

// createAdminPluginHandler serves POST /api/admin/plugins.
func (h *Handler) createAdminPluginHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !h.requirePlugins(w) {
		return
	}
	var req pluginwire.CreatePluginRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	entry, err := h.cfg.Plugins.CreateEntry(r.Context(), pluginsvc.CreateEntryInput{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Description: req.Description,
		ActorID:     actorID,
	})
	if err != nil {
		writePluginError(w, err, "admin_create_plugin", req.Name)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, entry)
}

// publishAdminPluginReleaseHandler serves
// POST /api/admin/plugins/{plugin_name}/releases.
//
// The body is the archive itself rather than a field inside a document, so the
// bytes stream from the connection to disk without a decoder holding them. The
// publisher's claim about where they came from travels as query parameters,
// which keeps the body one thing.
func (h *Handler) publishAdminPluginReleaseHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !h.requirePlugins(w) {
		return
	}
	name, ok := httputil.PathValue(w, r, "plugin_name")
	if !ok {
		return
	}
	q := r.URL.Query()
	dirty, _ := strconv.ParseBool(q.Get(pluginwire.QuerySourceDirty))

	release, err := h.cfg.Plugins.Publish(r.Context(), pluginsvc.PublishInput{
		PluginName: name,
		Body:       r.Body,
		Source: coreplugin.ReleaseSource{
			RemoteURL: q.Get(pluginwire.QuerySourceRemote),
			Commit:    q.Get(pluginwire.QuerySourceCommit),
			Branch:    q.Get(pluginwire.QuerySourceBranch),
			Dirty:     dirty,
		},
		ActorID: actorID,
	})
	if err != nil {
		writePluginError(w, err, "admin_publish_plugin_release", name)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, release)
}

// setAdminReleaseStateHandler serves
// PUT /api/admin/plugins/{plugin_name}/releases/{version}/state. Withdrawal is
// the only transition the store supports, so the body must set `yanked: true`.
// See the route conventions in docs/contribute/architecture/server.md
func (h *Handler) setAdminReleaseStateHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !h.requirePlugins(w) {
		return
	}
	name, ok := httputil.PathValue(w, r, "plugin_name")
	if !ok {
		return
	}
	version, ok := httputil.PathValue(w, r, "version")
	if !ok {
		return
	}
	var req pluginwire.ReleaseStateRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if !req.Yanked {
		// Restoring a withdrawn release is not a capability the catalog has, so
		// there is no meaningful non-withdrawn state to set here.
		httputil.WriteJSONError(w, http.StatusBadRequest, "un-yanking a release is not supported")
		return
	}
	if err := h.cfg.Plugins.Yank(r.Context(), name, version, actorID, req.Reason); err != nil {
		writePluginError(w, err, "admin_yank_plugin_release", name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// setAdminPluginStateHandler serves PUT /api/admin/plugins/{plugin_name}/state,
// setting the entry's stored `archived` flag.
func (h *Handler) setAdminPluginStateHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !h.requirePlugins(w) {
		return
	}
	name, ok := httputil.PathValue(w, r, "plugin_name")
	if !ok {
		return
	}
	var req pluginwire.PluginStateRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if err := h.cfg.Plugins.SetArchived(r.Context(), name, req.Archived, actorID); err != nil {
		writePluginError(w, err, "admin_set_plugin_archived", name)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requirePlugins refuses when the deployment has no Marketplace.
//
// It cannot go through httputil.RequireStore: every other store in Config is an
// interface, this one is a concrete pointer, and a typed nil pointer is not a
// nil `any` — the check would pass and the handler would dereference it.
func (h *Handler) requirePlugins(w http.ResponseWriter) bool {
	if h.cfg.Plugins == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "the plugin catalog is not configured")
		return false
	}
	return true
}

// writePluginError maps a service outcome to a status.
//
// A refused package is the caller's problem and says why; a version that is
// already published or an entry that is retired is a conflict rather than a bad
// request, because the same request would have succeeded a moment earlier.
func writePluginError(w http.ResponseWriter, err error, handler, name string) {
	switch {
	case errors.Is(err, archive.ErrTooLarge):
		httputil.WriteJSONError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, pluginsvc.ErrInvalidPackage), errors.Is(err, pluginsvc.ErrNameMismatch):
		httputil.WriteJSONError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, coreplugin.ErrVersionExists),
		errors.Is(err, coreplugin.ErrArchived),
		errors.Is(err, coreplugin.ErrNameTaken):
		httputil.WriteJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, apierr.ErrNotFound):
		httputil.WriteJSONError(w, http.StatusNotFound, "plugin not found")
	default:
		httputil.WriteInternalError(w, err, "handler error", "handler", handler, "plugin_name", name)
	}
}
