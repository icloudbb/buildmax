package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/page"
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

	// nav tracks the main frame's loading so an interaction can wait for the
	// navigation it triggers: a click that submits a form starts navigating
	// asynchronously, and reading the location right after it returns the page
	// being left.
	nav navState
}

// navState is the main frame's loading state. started counts navigations, so
// a caller can tell one began after it acted; idle is closed whenever the
// frame is not loading.
type navState struct {
	mu      sync.Mutex
	started uint64
	loading bool
	idle    chan struct{}
}

func (n *navState) begin() {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.started++
	if !n.loading {
		n.loading = true
		n.idle = make(chan struct{})
	}
}

func (n *navState) end() {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.loading {
		n.loading = false
		close(n.idle)
	}
}

// snapshot reports how many navigations have started and a channel closed
// once the frame is not loading.
func (n *navState) snapshot() (uint64, <-chan struct{}) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if !n.loading {
		done := make(chan struct{})
		close(done)
		return n.started, done
	}
	return n.started, n.idle
}

// navigationStartWindow is how long after an interaction a navigation it
// caused is expected to begin. A submit or a link starts within milliseconds;
// an interaction that navigates nothing pays this once.
const navigationStartWindow = 250 * time.Millisecond

// awaitNavigation waits, after an interaction, for a navigation that began
// since before (if one begins within navigationStartWindow) to finish loading,
// bounded by ctx.
func (p *chromedpPage) awaitNavigation(ctx context.Context, before uint64) {
	deadline := time.NewTimer(navigationStartWindow)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		started, idle := p.nav.snapshot()
		if started != before {
			select {
			case <-idle:
			case <-ctx.Done():
			}
			return
		}
		select {
		case <-deadline.C:
			return
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
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
	// sandbox the host forbids, a missing library, a locked profile. A launch
	// that hangs until browserStartTimeout has no other account of it.
	output := &outputTail{}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(c.execPath),
		chromedp.UserDataDir(dir),
		chromedp.WindowSize(viewportW, viewportH),
		chromedp.CombinedOutput(output),
	)
	if c.headful {
		// DefaultExecAllocatorOptions enables headless; a later flag wins, so
		// turn it back off to show a real window the user can watch. Not
		// chromedp.VisibleWindow: it maximizes the window, which overrides the
		// fixed viewport that screenshots and the reported size rely on.
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
	// Start the browser now so a launch failure (e.g. a broken executable) is
	// reported here rather than on the first navigation.
	if err := startBrowser(tabCtx); err != nil {
		ctxCancel()
		allocCancel()
		_ = os.RemoveAll(dir)
		return nil, nil, fmt.Errorf("start browser: %w%s", err, output.explain())
	}
	p.listen()
	return p, ctxCancel, nil
}

// startBrowser launches the browser and opens the tab, bounded by
// browserStartTimeout. chromedp talks to the browser over a pipe and waits for
// it without a limit, and a timeout on the first call's context would bound the
// browser's whole life, so the bound is a timer beside it; the caller cancels
// the tab and allocator contexts, which stops the launch, on failure.
func startBrowser(tabCtx context.Context) error {
	started := make(chan error, 1)
	go func() { started <- chromedp.Do(tabCtx) }()
	timer := time.NewTimer(browserStartTimeout)
	defer timer.Stop()
	select {
	case err := <-started:
		return err
	case <-timer.C:
		return fmt.Errorf("the browser did not start within %v", browserStartTimeout)
	}
}

// listen records console errors, uncaught exceptions, main-frame loading, and
// screencast frames. Each stream is its own subscription, read until the tab
// closes.
func (p *chromedpPage) listen() {
	// In Chrome a page target's main frame shares the target's id.
	mainFrame := cdp.FrameID("")
	if c := chromedp.FromContext(p.ctx); c != nil && c.Target != nil {
		mainFrame = cdp.FrameID(c.Target.TargetID)
	}
	isMain := func(id cdp.FrameID) bool { return mainFrame == "" || id == mainFrame }

	console := chromedp.Console(p.ctx)
	go func() {
		for m, err := range console {
			if err != nil {
				return
			}
			switch {
			case m.IsException():
				p.console.add("uncaught: " + m.Text)
			case m.Source == "" && (m.Type == chromedp.ConsoleError || m.Type == "assert"):
				// Source marks the browser log's own entries, such as failed
				// requests, which the page did not write.
				p.console.add("console." + string(m.Type) + ": " + m.Text)
			}
		}
	}()

	starts := chromedp.Events(p.ctx, page.FrameStartedLoading)
	stops := chromedp.Events(p.ctx, page.FrameStoppedLoading)
	go p.trackLoading(starts, stops, isMain)

	frames := chromedp.Events(p.ctx, page.ScreencastFrame)
	go func() {
		for e, err := range frames {
			if err != nil {
				return
			}
			// Ack so Chrome keeps sending frames.
			_, _ = chromedp.Call(p.ctx, page.ScreencastFrameAck, page.ScreencastFrameAckParams{SessionID: e.SessionID})
			p.frameMu.Lock()
			cb := p.onFrame
			p.frameMu.Unlock()
			if cb != nil {
				w, h := 0, 0
				if e.Metadata != nil {
					w, h = int(e.Metadata.DeviceWidth), int(e.Metadata.DeviceHeight)
				}
				cb(base64.StdEncoding.EncodeToString(e.Data), w, h)
			}
		}
	}()
}

// trackLoading feeds nav from the main frame's started and stopped loading
// events. Each subscription keeps its own order but not its order relative to
// the other, and the main frame alternates between the two, so reading only the
// one that comes next restores the sequence.
func (p *chromedpPage) trackLoading(starts iter.Seq2[page.EventFrameStartedLoading, error], stops iter.Seq2[page.EventFrameStoppedLoading, error], isMain func(cdp.FrameID) bool) {
	nextStart, stopStarts := iter.Pull2(starts)
	defer stopStarts()
	nextStop, stopStops := iter.Pull2(stops)
	defer stopStops()
	for {
		for {
			e, err, ok := nextStart()
			if !ok || err != nil {
				return
			}
			if isMain(e.FrameID) {
				break
			}
		}
		p.nav.begin()
		for {
			e, err, ok := nextStop()
			if !ok || err != nil {
				return
			}
			if isMain(e.FrameID) {
				break
			}
		}
		p.nav.end()
	}
}

// startScreencast turns on frame streaming; frames arrive via the listener.
func (p *chromedpPage) startScreencast(ctx context.Context, onFrame func(jpeg string, w, h int)) error {
	p.frameMu.Lock()
	p.onFrame = onFrame
	p.frameMu.Unlock()
	return p.run(ctx, call(page.StartScreencast, page.StartScreencastParams{
		Format:    page.StartScreencastFormatJpeg,
		Quality:   new(int64(50)),
		MaxWidth:  viewportW,
		MaxHeight: viewportH,
	}))
}

// stopScreencast turns off frame streaming and drops the callback.
func (p *chromedpPage) stopScreencast(ctx context.Context) error {
	p.frameMu.Lock()
	p.onFrame = nil
	p.frameMu.Unlock()
	return p.run(ctx, call(page.StopScreencast, cdp.Empty{}))
}

// call is an action that sends one protocol command and drops its result.
func call[P, R any](cmd cdp.Command[P, R], params P) chromedp.Action[chromedp.Void] {
	return chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		_, err := cdp.Call(ctx, t, cmd, params)
		return err
	})
}

// run executes chromedp actions with a bounded timeout, cancelling early if the
// incoming tool context is cancelled.
func (p *chromedpPage) run(ctx context.Context, actions ...chromedp.Action[chromedp.Void]) error {
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
	return chromedp.Do(opCtx, actions...)
}

// into is an action that stores the value of a, so values from several actions
// can be read in one bounded run.
func into[T any](dst *T, a chromedp.Action[T]) chromedp.Action[chromedp.Void] {
	return func(ctx context.Context, t *chromedp.Target) (chromedp.Void, error) {
		v, err := a(ctx, t)
		*dst = v
		return chromedp.Void{}, err
	}
}

func (p *chromedpPage) navigate(ctx context.Context, rawURL string) (string, string, int, error) {
	// Scope console errors to the page we are opening: drop what earlier pages
	// logged so BrowserConsole reflects the current page, not the tab's history.
	p.console.reset()
	var finalURL, title string
	err := p.run(ctx,
		chromedp.Navigate(rawURL),
		chromedp.WaitReady(chromedp.CSS("body")),
		into(&finalURL, chromedp.Location()),
		into(&title, chromedp.Title()),
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
		into(&res, chromedp.Evaluate[snapshotResult](snapshotJS)),
		into(&finalURL, chromedp.Location()),
		into(&title, chromedp.Title()),
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
	before, _ := p.nav.snapshot()
	if err := p.run(ctx, into(&res, chromedp.Evaluate[interactResult](expr))); err != nil {
		return false, "", "", err
	}
	if res.Found {
		waitCtx, cancel := context.WithTimeout(ctx, opTimeout)
		p.awaitNavigation(waitCtx, before)
		cancel()
	}
	if err := p.run(ctx, into(&finalURL, chromedp.Location()), into(&title, chromedp.Title())); err != nil {
		return false, "", "", err
	}
	return res.Found, finalURL, title, nil
}

func (p *chromedpPage) screenshot(ctx context.Context) ([]byte, string, string, error) {
	var buf []byte
	var finalURL, title string
	err := p.run(ctx,
		into(&buf, chromedp.CaptureScreenshot()),
		into(&finalURL, chromedp.Location()),
		into(&title, chromedp.Title()),
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

// browserStartTimeout is how long a launching browser gets to answer. A first
// launch builds its profile and font cache, and on a loaded CI runner that has
// taken 20 seconds (runs 35854939264 and 36081440975). A browser that cannot
// start still fails, with its output, when it exits.
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
