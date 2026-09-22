package browser

import (
	"context"
	"testing"
)

// fakeDriver is a scripted pageDriver: the controller's ownership, revision, and
// stale-reference logic is provable against it without a real browser.
type fakeDriver struct {
	url, title  string
	elems       []rawElement
	text        string
	found       bool   // interact: whether the element existed
	afterURL    string // interact: URL after the action, to simulate navigation
	console     []string
	closed      bool
	interactSel string // records the selector the last interact resolved to
}

func (d *fakeDriver) navigate(_ context.Context, rawURL string) (string, string, int, error) {
	d.url = rawURL
	return rawURL, d.title, 0, nil
}

func (d *fakeDriver) snapshot(_ context.Context) ([]rawElement, string, string, string, error) {
	return d.elems, d.text, d.url, d.title, nil
}

func (d *fakeDriver) interact(_ context.Context, _, selector, _ string) (bool, string, string, error) {
	d.interactSel = selector
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
