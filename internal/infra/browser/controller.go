package browser

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/icloudbb/buildmax/internal/tool"
)

// rawElement is one interactive element as the page driver reports it, before
// the controller turns it into a session-scoped reference.
type rawElement struct {
	Ref   string // e.g. "e3"; also the data attribute the driver tagged the node with
	Role  string
	Name  string
	Value string
}

// pageDriver is the browser-touching half of one page. The chromedp
// implementation is chromedpPage; tests substitute a fake so the controller's
// ownership, revision, and stale-reference logic is provable without a browser.
type pageDriver interface {
	navigate(ctx context.Context, rawURL string) (finalURL, title string, status int, err error)
	snapshot(ctx context.Context) (elems []rawElement, text, finalURL, title string, err error)
	// interact performs "click" or "type" against selector; found reports
	// whether the element existed (a stale or replaced reference does not).
	interact(ctx context.Context, action, selector, text string) (found bool, finalURL, title string, err error)
	screenshot(ctx context.Context) (png []byte, finalURL, title string, err error)
	consoleErrors() []string
	viewport() string
	close()
}

// sessionPage is one BuildMax session's page and the controller's bookkeeping
// for it. refs are the references handed out by the last snapshot; they are
// valid only while revision equals refRevision.
type sessionPage struct {
	driver      pageDriver
	cancel      func()
	revision    int
	url         string
	title       string
	refs        map[string]string // ref -> CSS selector
	refRevision int
}

// Controller owns browser discovery, process lifecycle, and the map from a
// BuildMax session to its page. It implements tool.BrowserController. The
// browser process is launched lazily on the first navigation.
type Controller struct {
	execPath string

	mu       sync.Mutex
	sessions map[string]*sessionPage

	// newPage creates a fresh page — its own browser process and isolated
	// profile — for one session. Overridable in tests to avoid a real browser;
	// nil means the chromedp implementation is used.
	newPage func(ctx context.Context) (pageDriver, func(), error)
}

var _ tool.BrowserController = (*Controller)(nil)

// New discovers a system Chrome/Edge and returns a controller ready to launch it
// lazily. It fails now, with a clear error, when no browser is installed, rather
// than at the first tool call deep in a run.
func New() (*Controller, error) {
	f := finder{goos: runtimeGOOS(), lookPath: lookPath, isFile: isRegularFile}
	execPath, err := f.find()
	if err != nil {
		return nil, err
	}
	return &Controller{execPath: execPath, sessions: map[string]*sessionPage{}}, nil
}

// validateNavURL admits only http(s) origins with a host. It rejects file:,
// javascript:, data:, and browser-internal schemes before anything is loaded.
func validateNavURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid url %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported url scheme %q: only http and https are allowed", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("url %q has no host", raw)
	}
	return u.String(), nil
}

// ensureSession returns the session's page, creating it (and launching the
// browser on first use) when absent.
func (c *Controller) ensureSession(ctx context.Context, sessionID string) (*sessionPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sp, ok := c.sessions[sessionID]; ok {
		return sp, nil
	}
	factory := c.newPage
	if factory == nil {
		factory = c.newChromedpPage
	}
	driver, cancel, err := factory(ctx)
	if err != nil {
		return nil, err
	}
	sp := &sessionPage{driver: driver, cancel: cancel}
	c.sessions[sessionID] = sp
	return sp, nil
}

// existingSession returns the session's page or an error; observation and
// interaction never create a page, so an Agent must BrowserNavigate first.
func (c *Controller) existingSession(sessionID string) (*sessionPage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	sp, ok := c.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("no browser page for this session; call %s first", tool.ToolNameBrowserNavigate)
	}
	return sp, nil
}

func (c *Controller) Navigate(ctx context.Context, sessionID, rawURL string) (tool.PageState, error) {
	clean, err := validateNavURL(rawURL)
	if err != nil {
		return tool.PageState{}, err
	}
	sp, err := c.ensureSession(ctx, sessionID)
	if err != nil {
		return tool.PageState{}, err
	}
	finalURL, title, status, err := sp.driver.navigate(ctx, clean)
	if err != nil {
		return tool.PageState{}, fmt.Errorf("navigate to %q: %w", clean, err)
	}
	sp.revision++
	sp.refs = nil
	sp.url, sp.title = finalURL, title
	return c.stateOf(sp, status), nil
}

func (c *Controller) Snapshot(ctx context.Context, sessionID string) (tool.PageSnapshot, error) {
	sp, err := c.existingSession(sessionID)
	if err != nil {
		return tool.PageSnapshot{}, err
	}
	elems, text, finalURL, title, err := sp.driver.snapshot(ctx)
	if err != nil {
		return tool.PageSnapshot{}, fmt.Errorf("snapshot: %w", err)
	}
	sp.url, sp.title = finalURL, title
	refs := make(map[string]string, len(elems))
	out := make([]tool.Element, 0, len(elems))
	for _, e := range elems {
		refs[e.Ref] = fmt.Sprintf("[data-bm-ref=%q]", e.Ref)
		out = append(out, tool.Element{Ref: e.Ref, Role: e.Role, Name: e.Name, Value: e.Value})
	}
	sp.refs = refs
	sp.refRevision = sp.revision
	return tool.PageSnapshot{PageState: c.stateOf(sp, 0), Text: text, Elements: out}, nil
}

func (c *Controller) Click(ctx context.Context, sessionID, ref string) (tool.PageState, error) {
	return c.act(ctx, sessionID, "click", ref, "")
}

func (c *Controller) Type(ctx context.Context, sessionID, ref, text string) (tool.PageState, error) {
	return c.act(ctx, sessionID, "type", ref, text)
}

// act resolves a reference against the current revision and drives one
// interaction, rejecting a reference from an older revision or a vanished node
// as stale rather than acting on a guessed target.
func (c *Controller) act(ctx context.Context, sessionID, action, ref, text string) (tool.PageState, error) {
	sp, err := c.existingSession(sessionID)
	if err != nil {
		return tool.PageState{}, err
	}
	if sp.refs == nil || sp.refRevision != sp.revision {
		return tool.PageState{}, fmt.Errorf("no current snapshot; call %s before interacting", tool.ToolNameBrowserSnapshot)
	}
	selector, ok := sp.refs[ref]
	if !ok {
		return tool.PageState{}, fmt.Errorf("unknown element reference %q; take a fresh %s", ref, tool.ToolNameBrowserSnapshot)
	}
	found, finalURL, title, err := sp.driver.interact(ctx, action, selector, text)
	if err != nil {
		return tool.PageState{}, fmt.Errorf("%s %q: %w", action, ref, err)
	}
	if !found {
		return tool.PageState{}, fmt.Errorf("element reference %q is stale; the page changed, take a fresh %s", ref, tool.ToolNameBrowserSnapshot)
	}
	if finalURL != sp.url {
		// The interaction navigated: the old references no longer describe the page.
		sp.revision++
		sp.refs = nil
	}
	sp.url, sp.title = finalURL, title
	return c.stateOf(sp, 0), nil
}

func (c *Controller) Screenshot(ctx context.Context, sessionID string) (tool.Screenshot, error) {
	sp, err := c.existingSession(sessionID)
	if err != nil {
		return tool.Screenshot{}, err
	}
	png, finalURL, title, err := sp.driver.screenshot(ctx)
	if err != nil {
		return tool.Screenshot{}, fmt.Errorf("screenshot: %w", err)
	}
	sp.url, sp.title = finalURL, title
	return tool.Screenshot{PageState: c.stateOf(sp, 0), PNG: png}, nil
}

func (c *Controller) ConsoleErrors(ctx context.Context, sessionID string) ([]string, error) {
	sp, err := c.existingSession(sessionID)
	if err != nil {
		return nil, err
	}
	return sp.driver.consoleErrors(), nil
}

// stateOf builds the PageState reported to the tool layer from the session's
// current bookkeeping.
func (c *Controller) stateOf(sp *sessionPage, status int) tool.PageState {
	return tool.PageState{
		URL:      sp.url,
		Title:    sp.title,
		Status:   status,
		Viewport: sp.driver.viewport(),
		Revision: sp.revision,
	}
}

// Close releases every session's page and the browser process and temporary
// profile. It is safe to call more than once.
func (c *Controller) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, sp := range c.sessions {
		if sp.cancel != nil {
			sp.cancel()
		}
		sp.driver.close()
		delete(c.sessions, id)
	}
	return nil
}
