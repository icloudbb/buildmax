package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

const (
	// opTimeout bounds a single browser operation so a hung page surfaces as a
	// tool error the Agent can diagnose rather than blocking the run.
	opTimeout = 30 * time.Second
	// maxConsoleErrors bounds the retained console-error ring per page.
	maxConsoleErrors = 50
	// maxSnapshotText bounds the page text an observation carries into context.
	maxSnapshotText = 4000
	// viewportW and viewportH fix the headless window so screenshots and layout
	// are reproducible.
	viewportW, viewportH = 1280, 800
)

// chromedpPage is the chromedp-backed pageDriver: one browser process, one
// isolated profile, one tab, for one session.
type chromedpPage struct {
	ctx         context.Context
	allocCancel context.CancelFunc
	ctxCancel   context.CancelFunc
	userDataDir string

	console *consoleRing

	frameMu sync.Mutex
	onFrame func(jpeg string, w, h int) // set while a screencast is running
}

// consoleRing is a bounded, concurrency-safe buffer of recent console errors.
type consoleRing struct {
	mu   sync.Mutex
	msgs []string
}

func (r *consoleRing) add(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if len(s) > 500 {
		s = s[:500] + "…"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, s)
	if len(r.msgs) > maxConsoleErrors {
		r.msgs = r.msgs[len(r.msgs)-maxConsoleErrors:]
	}
}

func (r *consoleRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.msgs))
	copy(out, r.msgs)
	return out
}

// reset drops accumulated messages. Called on navigation so BrowserConsole
// reports the current page's errors rather than every page the tab has visited.
func (r *consoleRing) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = nil
}

// newChromedpPage launches a browser process with a fresh, isolated user-data
// directory and opens one tab, returning the driver and a cancel that tears the
// tab down. The heavier teardown (process, profile) is in close.
func (c *Controller) newChromedpPage(_ context.Context) (pageDriver, func(), error) {
	dir, err := os.MkdirTemp("", "buildmax-browser-")
	if err != nil {
		return nil, nil, fmt.Errorf("create browser profile dir: %w", err)
	}
	// Chrome's own output is the only account of why it failed to start: a
	// sandbox the host forbids, a missing library, a locked profile. Without it
	// a launch failure reads only as chromedp's "websocket url timeout".
	output := &outputTail{}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(c.execPath),
		chromedp.UserDataDir(dir),
		chromedp.WindowSize(viewportW, viewportH),
		chromedp.CombinedOutput(output),
		// chromedp waits 20 seconds by default for Chrome to print its DevTools
		// address. A first launch builds its profile and font cache, and on a
		// loaded CI runner that has taken the whole 20 seconds while Chrome was
		// still starting (runs 35854939264 and 36081440975, both failing at
		// exactly 20.0s). A browser that cannot start still fails, with its
		// output, when it exits.
		chromedp.WSURLReadTimeout(browserStartTimeout),
	)
	if c.headful {
		// DefaultExecAllocatorOptions enables headless; a later flag wins, so
		// turn it back off to show a real window the user can watch.
		opts = append(opts, chromedp.Flag("headless", false))
	}
	// context.Background so the browser outlives the triggering tool call; its
	// lifetime is owned by the controller and released in close/Close.
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	tabCtx, ctxCancel := chromedp.NewContext(allocCtx)

	p := &chromedpPage{
		ctx:         tabCtx,
		allocCancel: allocCancel,
		ctxCancel:   ctxCancel,
		userDataDir: dir,
		console:     &consoleRing{},
	}
	p.listen()
	// Start the browser now so a launch failure (e.g. a broken executable) is
	// reported here rather than on the first navigation.
	if err := chromedp.Run(tabCtx); err != nil {
		ctxCancel()
		allocCancel()
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("start browser: %w%s", err, output.explain())
	}
	return p, ctxCancel, nil
}

// listen records console errors and uncaught exceptions into the ring.
func (p *chromedpPage) listen() {
	chromedp.ListenTarget(p.ctx, func(ev any) {
		switch e := ev.(type) {
		case *runtime.EventConsoleAPICalled:
			if e.Type != "error" && e.Type != "assert" {
				return
			}
			parts := make([]string, 0, len(e.Args))
			for _, a := range e.Args {
				parts = append(parts, remoteObjectString(a))
			}
			p.console.add("console." + string(e.Type) + ": " + strings.Join(parts, " "))
		case *runtime.EventExceptionThrown:
			if e.ExceptionDetails != nil {
				p.console.add("uncaught: " + e.ExceptionDetails.Text)
			}
		case *page.EventScreencastFrame:
			// Ack so Chrome keeps sending frames; do it off the listener so a
			// slow ack never stalls event delivery.
			go func(sid int64) { _ = chromedp.Run(p.ctx, page.ScreencastFrameAck(sid)) }(e.SessionID)
			p.frameMu.Lock()
			cb := p.onFrame
			p.frameMu.Unlock()
			if cb != nil {
				w, h := 0, 0
				if e.Metadata != nil {
					w, h = int(e.Metadata.DeviceWidth), int(e.Metadata.DeviceHeight)
				}
				cb(e.Data, w, h)
			}
		}
	})
}

// startScreencast turns on frame streaming; frames arrive via the listener.
func (p *chromedpPage) startScreencast(ctx context.Context, onFrame func(jpeg string, w, h int)) error {
	p.frameMu.Lock()
	p.onFrame = onFrame
	p.frameMu.Unlock()
	return p.run(ctx, page.StartScreencast().
		WithFormat(page.ScreencastFormatJpeg).
		WithQuality(50).
		WithMaxWidth(viewportW).
		WithMaxHeight(viewportH))
}

// stopScreencast turns off frame streaming and drops the callback.
func (p *chromedpPage) stopScreencast(ctx context.Context) error {
	p.frameMu.Lock()
	p.onFrame = nil
	p.frameMu.Unlock()
	return p.run(ctx, page.StopScreencast())
}

func remoteObjectString(o *runtime.RemoteObject) string {
	if o == nil {
		return ""
	}
	if len(o.Value) > 0 {
		var s string
		if err := json.Unmarshal(o.Value, &s); err == nil {
			return s
		}
		return string(o.Value)
	}
	return o.Description
}

// run executes chromedp actions with a bounded timeout, cancelling early if the
// incoming tool context is cancelled.
func (p *chromedpPage) run(ctx context.Context, actions ...chromedp.Action) error {
	opCtx, cancel := context.WithTimeout(p.ctx, opTimeout)
	defer cancel()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			cancel()
		case <-done:
		}
	}()
	return chromedp.Run(opCtx, actions...)
}

func (p *chromedpPage) navigate(ctx context.Context, rawURL string) (string, string, int, error) {
	// Scope console errors to the page we are opening: drop what earlier pages
	// logged so BrowserConsole reflects the current page, not the tab's history.
	p.console.reset()
	var finalURL, title string
	err := p.run(ctx,
		chromedp.Navigate(rawURL),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	)
	if err != nil {
		return "", "", 0, err
	}
	return finalURL, title, 0, nil
}

// snapshotResult mirrors the JSON the snapshot script returns.
type snapshotResult struct {
	Elements []rawElement `json:"elements"`
	Text     string       `json:"text"`
}

func (p *chromedpPage) snapshot(ctx context.Context) ([]rawElement, string, string, string, error) {
	var res snapshotResult
	var finalURL, title string
	err := p.run(ctx,
		chromedp.Evaluate(snapshotJS, &res),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	)
	if err != nil {
		return nil, "", "", "", err
	}
	if len(res.Text) > maxSnapshotText {
		res.Text = res.Text[:maxSnapshotText] + "…"
	}
	return res.Elements, res.Text, finalURL, title, nil
}

// interactResult mirrors the JSON the click/type scripts return.
type interactResult struct {
	Found bool `json:"found"`
}

func (p *chromedpPage) interact(ctx context.Context, action, selector, text, origin string) (bool, string, string, error) {
	sel, _ := json.Marshal(selector)
	org, _ := json.Marshal(origin)
	var expr string
	switch action {
	case "click":
		expr = fmt.Sprintf(clickJS, string(org), string(sel))
	case "type":
		txt, _ := json.Marshal(text)
		expr = fmt.Sprintf(typeJS, string(org), string(sel), string(txt))
	default:
		return false, "", "", fmt.Errorf("unknown action %q", action)
	}
	var res interactResult
	var finalURL, title string
	err := p.run(ctx,
		chromedp.Evaluate(expr, &res),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	)
	if err != nil {
		return false, "", "", err
	}
	return res.Found, finalURL, title, nil
}

func (p *chromedpPage) screenshot(ctx context.Context) ([]byte, string, string, error) {
	var buf []byte
	var finalURL, title string
	err := p.run(ctx,
		chromedp.CaptureScreenshot(&buf),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
	)
	if err != nil {
		return nil, "", "", err
	}
	return buf, finalURL, title, nil
}

func (p *chromedpPage) consoleErrors() []string { return p.console.snapshot() }

func (p *chromedpPage) viewport() string { return fmt.Sprintf("%dx%d", viewportW, viewportH) }

// close releases the browser process and removes the temporary profile.
func (p *chromedpPage) close() {
	if p.ctxCancel != nil {
		p.ctxCancel()
	}
	if p.allocCancel != nil {
		p.allocCancel()
	}
	if p.userDataDir != "" {
		_ = os.RemoveAll(p.userDataDir)
	}
}

// snapshotJS tags each visible interactive element with a stable data-bm-ref and
// returns the references with their role, accessible name, and value, plus a
// bounded slice of page text.
const snapshotJS = `(() => {
  const sel = 'a,button,input,textarea,select,[role],[onclick],[contenteditable="true"]';
  const out = [];
  let i = 0;
  for (const n of document.querySelectorAll(sel)) {
    const r = n.getBoundingClientRect();
    if (r.width === 0 && r.height === 0) continue;
    const ref = 'e' + (++i);
    n.setAttribute('data-bm-ref', ref);
    const role = n.getAttribute('role') || n.tagName.toLowerCase();
    const name = (n.getAttribute('aria-label') || n.getAttribute('placeholder') || n.getAttribute('name') || (n.textContent || '').trim()).slice(0, 120);
    const value = ((n.value != null ? String(n.value) : '')).slice(0, 120);
    out.push({ref, role, name, value});
    if (i >= 200) break;
  }
  const text = (document.body ? document.body.innerText : '').slice(0, 8000);
  return {elements: out, text};
})()`

// clickJS clicks the referenced element, reporting whether it existed. It acts
// only on the admitted origin: a document elsewhere could carry a planted
// data-bm-ref that matches the selector.
const clickJS = `(() => {
  if (location.origin !== %s) return {found: false};
  const el = document.querySelector(%s);
  if (!el) return {found: false};
  el.scrollIntoView({block: 'center'});
  el.click();
  return {found: true};
})()`

// typeJS sets the referenced element's value and dispatches input/change so
// frameworks observe the edit, reporting whether it existed. Confined to the
// admitted origin, as clickJS is.
const typeJS = `(() => {
  if (location.origin !== %s) return {found: false};
  const el = document.querySelector(%s);
  if (!el) return {found: false};
  el.focus();
  el.value = %s;
  el.dispatchEvent(new Event('input', {bubbles: true}));
  el.dispatchEvent(new Event('change', {bubbles: true}));
  return {found: true};
})()`

// browserStartTimeout is how long a launching browser gets to report its
// DevTools address.
const browserStartTimeout = 60 * time.Second

// outputTailBytes bounds what a failed launch reports: enough for Chrome's
// closing lines, not its whole log.
const outputTailBytes = 2048

// outputTail keeps the last bytes a browser process wrote. Chrome writes from
// its own goroutines for as long as it runs, so it is safe for concurrent use.
type outputTail struct {
	mu  sync.Mutex
	buf []byte
}

func (o *outputTail) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf = append(o.buf, p...)
	if extra := len(o.buf) - outputTailBytes; extra > 0 {
		o.buf = append(o.buf[:0], o.buf[extra:]...)
	}
	return len(p), nil
}

// explain renders the captured output for an error, naming the one cause a
// reader cannot guess from Chrome's wording alone.
func (o *outputTail) explain() string {
	o.mu.Lock()
	text := strings.TrimSpace(string(o.buf))
	o.mu.Unlock()
	if text == "" {
		return ""
	}
	hint := ""
	if strings.Contains(text, "No usable sandbox") {
		hint = "\nthe host does not allow Chrome's sandbox; on Linux this is usually " +
			"kernel.apparmor_restrict_unprivileged_userns=1 without an AppArmor profile for this browser"
	}
	return "\nbrowser output:\n" + text + hint
}
