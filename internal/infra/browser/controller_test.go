package browser

import (
	"context"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/tool"
)

// fakeDriver is a scripted pageDriver: the controller's ownership, revision, and
// stale-reference logic is provable against it without a real browser.
type fakeDriver struct {
	url, title   string
	elems        []rawElement
	text         string
	found        bool   // interact: whether the element existed
	afterURL     string // interact: URL after the action, to simulate navigation
	redirectTo   string // navigate: final URL, to simulate a server redirect
	console      []string
	closed       bool
	interactSel  string // records the selector the last interact resolved to
	acted        int    // interactions actually performed on the page
	screencastOn bool
	onFrame      func(jpeg string, w, h int)
}

func (d *fakeDriver) navigate(_ context.Context, rawURL string) (string, string, int, error) {
	d.url = rawURL
	if d.redirectTo != "" {
		d.url = d.redirectTo
	}
	return d.url, d.title, 0, nil
}

func (d *fakeDriver) snapshot(_ context.Context) ([]rawElement, string, string, string, error) {
	return d.elems, d.text, d.url, d.title, nil
}

// interact mirrors the real driver's origin guard: it refuses to act when the
// live document is not on origin.
func (d *fakeDriver) interact(_ context.Context, _, selector, _, origin string) (bool, string, string, error) {
	if cur, err := tool.BrowserOrigin(d.url); err != nil || cur != origin {
		return false, d.url, d.title, nil
	}
	d.interactSel = selector
	d.acted++
	next := d.url
	if d.afterURL != "" {
		next = d.afterURL
	}
	d.url = next
	return d.found, next, d.title, nil
}

func (d *fakeDriver) screenshot(_ context.Context) ([]byte, string, string, error) {
	return []byte("PNGDATA"), d.url, d.title, nil
}

func (d *fakeDriver) consoleErrors() []string { return d.console }
func (d *fakeDriver) viewport() string        { return "1280x800" }
func (d *fakeDriver) close()                  { d.closed = true }

func (d *fakeDriver) startScreencast(_ context.Context, onFrame func(jpeg string, w, h int)) error {
	d.screencastOn = true
	d.onFrame = onFrame
	return nil
}

func (d *fakeDriver) stopScreencast(_ context.Context) error {
	d.screencastOn = false
	d.onFrame = nil
	return nil
}

// newTestController wires a controller whose pages are fresh fakeDrivers,
// returning the controller and a map recording the driver created per session
// (in creation order) so a test can inspect them.
func newTestController(prep func(*fakeDriver)) (*Controller, *[]*fakeDriver) {
	created := &[]*fakeDriver{}
	c := &Controller{
		sessions: map[string]*sessionPage{},
		newPage: func(context.Context) (pageDriver, func(), error) {
			d := &fakeDriver{title: "Test", found: true}
			if prep != nil {
				prep(d)
			}
			*created = append(*created, d)
			return d, func() {}, nil
		},
	}
	return c, created
}

func TestNavigateRejectsNonHTTPSchemes(t *testing.T) {
	c, _ := newTestController(nil)
	for _, bad := range []string{"file:///etc/passwd", "javascript:alert(1)", "data:text/html,x", "chrome://settings"} {
		if _, err := c.Navigate(context.Background(), "s1", bad); err == nil {
			t.Errorf("Navigate(%q) = nil error, want rejection", bad)
		}
	}
}

func TestObservationBeforeNavigateIsAnError(t *testing.T) {
	c, _ := newTestController(nil)
	if _, err := c.Snapshot(context.Background(), "s1"); err == nil {
		t.Error("Snapshot before Navigate should error")
	}
	if _, err := c.Click(context.Background(), "s1", "e1"); err == nil {
		t.Error("Click before Navigate should error")
	}
}

func TestSessionsAreIsolated(t *testing.T) {
	c, _ := newTestController(nil)
	if _, err := c.Navigate(context.Background(), "sessionA", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate A: %v", err)
	}
	// Session B never navigated, so it has no page and cannot observe A's.
	if _, err := c.Snapshot(context.Background(), "sessionB"); err == nil {
		t.Error("session B must not reach session A's page")
	}
}

func TestSnapshotRefsThenClick(t *testing.T) {
	c, _ := newTestController(func(d *fakeDriver) {
		d.elems = []rawElement{{Ref: "e1", Role: "button", Name: "Submit"}}
	})
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	snap, err := c.Snapshot(ctx, "s1")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(snap.Elements) != 1 || snap.Elements[0].Ref != "e1" {
		t.Fatalf("snapshot elements = %+v", snap.Elements)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err != nil {
		t.Fatalf("Click e1: %v", err)
	}
	// An unknown reference is rejected, not guessed.
	if _, err := c.Click(ctx, "s1", "e9"); err == nil {
		t.Error("Click of unknown ref should error")
	}
}

func TestClickWithoutSnapshotIsRejected(t *testing.T) {
	c, _ := newTestController(nil)
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err == nil {
		t.Error("Click without a prior snapshot should error")
	}
}

func TestReferencesGoStaleAfterNavigation(t *testing.T) {
	c, _ := newTestController(func(d *fakeDriver) {
		d.elems = []rawElement{{Ref: "e1", Role: "link", Name: "Next"}}
		d.afterURL = "http://localhost:1/page2" // clicking navigates
	})
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err != nil {
		t.Fatalf("first Click: %v", err)
	}
	// The click navigated, so the old reference must now be stale.
	if _, err := c.Click(ctx, "s1", "e1"); err == nil {
		t.Error("reference should be stale after navigation")
	}
}

func TestStaleElementReportedAsStale(t *testing.T) {
	c, _ := newTestController(func(d *fakeDriver) {
		d.elems = []rawElement{{Ref: "e1", Role: "button"}}
		d.found = false // element vanished from the DOM
	})
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err == nil {
		t.Error("a vanished element should be reported as stale")
	}
}

func TestScreenshotAndConsolePassThrough(t *testing.T) {
	c, _ := newTestController(func(d *fakeDriver) {
		d.console = []string{"console.error: boom"}
	})
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	shot, err := c.Screenshot(ctx, "s1")
	if err != nil {
		t.Fatalf("Screenshot: %v", err)
	}
	if string(shot.PNG) != "PNGDATA" {
		t.Errorf("screenshot png = %q", shot.PNG)
	}
	errs, err := c.ConsoleErrors(ctx, "s1")
	if err != nil {
		t.Fatalf("ConsoleErrors: %v", err)
	}
	if len(errs) != 1 || errs[0] != "console.error: boom" {
		t.Errorf("console errors = %v", errs)
	}
}

func TestObserverReceivesPageChangesAndClose(t *testing.T) {
	c, _ := newTestController(nil)
	var events []Event
	c.SetObserver(func(e Event) { events = append(events, e) })
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/home"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if len(events) != 1 || events[0].SessionID != "s1" || events[0].URL != "http://localhost:1/home" || events[0].Closed {
		t.Fatalf("navigate event = %+v", events)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	last := events[len(events)-1]
	if last.SessionID != "s1" || !last.Closed {
		t.Errorf("expected a closed event for s1, got %+v", events)
	}
}

func TestScreencastStartsOnlyWithFrameObserver(t *testing.T) {
	// No frame observer (CLI): navigation must not start a screencast.
	c, created := newTestController(nil)
	if _, err := c.Navigate(context.Background(), "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if (*created)[0].screencastOn {
		t.Error("screencast should not start without a frame observer")
	}

	// With a frame observer (Desktop): navigation starts a screencast and frames
	// reach the observer tagged with the session.
	c2, created2 := newTestController(nil)
	var frames []Frame
	c2.SetFrameObserver(func(f Frame) { frames = append(frames, f) })
	if _, err := c2.Navigate(context.Background(), "sX", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	d := (*created2)[0]
	if !d.screencastOn || d.onFrame == nil {
		t.Fatal("screencast should start when a frame observer is set")
	}
	d.onFrame("BASE64JPEG", 1280, 800)
	if len(frames) != 1 || frames[0].SessionID != "sX" || frames[0].JPEG != "BASE64JPEG" {
		t.Errorf("frame = %+v", frames)
	}
}

func TestCloseReleasesPages(t *testing.T) {
	c, created := newTestController(nil)
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if len(*created) != 1 || !(*created)[0].closed {
		t.Error("Close should have closed the session's driver")
	}
	// After Close the session is gone; a later observation errors rather than
	// reaching a released page.
	if _, err := c.Snapshot(ctx, "s1"); err == nil {
		t.Error("session should be gone after Close")
	}
}

// TestClickThatLeavesOriginConfinesInteraction: a click on an approved page that
// navigates to another origin is reported, and the next click or type there is
// refused until BrowserNavigate — the call the approval gate scopes per origin —
// opens that origin itself.
func TestClickThatLeavesOriginConfinesInteraction(t *testing.T) {
	c, created := newTestController(func(d *fakeDriver) {
		d.elems = []rawElement{{Ref: "e1", Role: "link", Name: "Elsewhere"}}
		d.afterURL = "https://evil.example/landing"
	})
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	state, err := c.Click(ctx, "s1", "e1")
	if err != nil {
		t.Fatalf("Click on the admitted origin: %v", err)
	}
	if state.URL != "https://evil.example/landing" || state.AdmittedOrigin != "http://localhost:1" {
		t.Fatalf("state = %+v, want the new URL reported against the admitted origin", state)
	}

	// Observing the new page is allowed; its content is data.
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot off-origin: %v", err)
	}
	d := (*created)[0]
	for name, act := range map[string]func() error{
		"Click": func() error { _, err := c.Click(ctx, "s1", "e1"); return err },
		"Type":  func() error { _, err := c.Type(ctx, "s1", "e1", "secret"); return err },
	} {
		err := act()
		if err == nil || !strings.Contains(err.Error(), "https://evil.example") || !strings.Contains(err.Error(), tool.ToolNameBrowserNavigate) {
			t.Errorf("%s off-origin = %v, want a refusal naming the origin and %s", name, err, tool.ToolNameBrowserNavigate)
		}
	}
	if d.acted != 1 {
		t.Errorf("driver acted %d times, want 1 (nothing on the unapproved origin)", d.acted)
	}

	// Opening the origin through BrowserNavigate admits it.
	d.afterURL = ""
	if _, err := c.Navigate(ctx, "s1", "https://evil.example/landing"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err != nil {
		t.Errorf("Click after BrowserNavigate admitted the origin: %v", err)
	}
}

// TestNavigateRedirectAdmitsOnlyRequestedOrigin: the prompt showed the
// requested URL, so a server redirect to another origin is not admitted.
func TestNavigateRedirectAdmitsOnlyRequestedOrigin(t *testing.T) {
	c, created := newTestController(func(d *fakeDriver) {
		d.elems = []rawElement{{Ref: "e1", Role: "button"}}
		d.redirectTo = "http://127.0.0.1:2/login"
	})
	ctx := context.Background()
	state, err := c.Navigate(ctx, "s1", "http://localhost:1/")
	if err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if state.AdmittedOrigin != "http://localhost:1" {
		t.Errorf("admitted origin = %q, want the requested one", state.AdmittedOrigin)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err == nil {
		t.Error("Click on a redirect target that was never approved should be refused")
	}
	if (*created)[0].acted != 0 {
		t.Error("driver acted on an unapproved origin")
	}
}

// TestInteractionRefusedWhenPageLeftOriginUnseen: a page that navigated itself
// after the snapshot is caught by the driver's guard, and reported as an origin
// refusal rather than a stale reference.
func TestInteractionRefusedWhenPageLeftOriginUnseen(t *testing.T) {
	c, created := newTestController(func(d *fakeDriver) {
		d.elems = []rawElement{{Ref: "e1", Role: "button"}}
	})
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "http://localhost:1/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	d := (*created)[0]
	d.url = "https://evil.example/" // a script redirect the controller has not observed
	_, err := c.Type(ctx, "s1", "e1", "secret")
	if err == nil || !strings.Contains(err.Error(), "https://evil.example") {
		t.Errorf("Type = %v, want an origin refusal", err)
	}
	if d.acted != 0 {
		t.Error("driver acted on an unapproved origin")
	}
}

// TestSameOriginIsNormalized: scheme and host case and a default port do not
// make the page look like it left the origin it was opened on.
func TestSameOriginIsNormalized(t *testing.T) {
	c, _ := newTestController(func(d *fakeDriver) {
		d.elems = []rawElement{{Ref: "e1", Role: "button"}}
		d.afterURL = "http://localhost/next"
	})
	ctx := context.Background()
	if _, err := c.Navigate(ctx, "s1", "HTTP://LocalHost:80/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err != nil {
		t.Fatalf("Click: %v", err)
	}
	if _, err := c.Snapshot(ctx, "s1"); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if _, err := c.Click(ctx, "s1", "e1"); err != nil {
		t.Errorf("Click after a same-origin navigation = %v, want it allowed", err)
	}
}
