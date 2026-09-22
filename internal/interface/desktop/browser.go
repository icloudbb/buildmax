package desktop

import "github.com/icloudbb/buildmax/internal/infra/browser"

// eventBrowserState reports which page a session's browser is showing so the
// frontend can display a compact activity indicator. The browser opens as its
// own visible OS window the user can watch; this event only links the session
// to its current page (and clears when the page is released). Embedding the
// page inside the workspace is a separate, deferred decision. See
// docs/design/agent-browser-capability.md.
const eventBrowserState = "desktop/browser/state"

// BrowserStatePayload is the current page of one session's browser, or a closed
// marker when its page was released.
type BrowserStatePayload struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Closed    bool   `json:"closed"`
}

// browserObserver forwards controller page changes to the frontend. It resolves
// a.ctx at call time, matching the terminal emitter, so an event fired before
// Startup still targets the live window context.
func (a *App) browserObserver(ev browser.Event) {
	a.emit(a.ctx, eventBrowserState, BrowserStatePayload{
		SessionID: ev.SessionID,
		URL:       ev.URL,
		Title:     ev.Title,
		Closed:    ev.Closed,
	})
}
