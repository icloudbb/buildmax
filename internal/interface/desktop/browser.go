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

// eventBrowserFrame streams one screencast frame of a session's live page so a
// workspace tab can render it. The image is the real CDP-controlled page, not a
// second instance; embedding is read-only in this slice.
const eventBrowserFrame = "desktop/browser/frame"

// BrowserFramePayload is one JPEG frame of a session's page. Data is base64 with
// no data: prefix; Width and Height are the frame's device size.
type BrowserFramePayload struct {
	SessionID string `json:"session_id"`
	Data      string `json:"data"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// browserFrameObserver forwards screencast frames to the frontend.
func (a *App) browserFrameObserver(f browser.Frame) {
	a.emit(a.ctx, eventBrowserFrame, BrowserFramePayload{
		SessionID: f.SessionID,
		Data:      f.JPEG,
		Width:     f.Width,
		Height:    f.Height,
	})
}
