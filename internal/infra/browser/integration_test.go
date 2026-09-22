package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testApp is a tiny local web app with a stateful form, a route change, and a
// deliberate console error — the journey the design record's prototype exercises.
func testApp() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><body>
			<h1>Login</h1>
			<form action="/welcome" method="get">
				<input name="user" aria-label="Username">
				<button type="submit">Sign in</button>
			</form>
			<script>console.error("deliberate boom");</script>
		</body></html>`))
	})
	mux.HandleFunc("/welcome", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><html><body>
			<h1 id="hello">Welcome ` + r.URL.Query().Get("user") + `</h1>
		</body></html>`))
	})
	return mux
}

// TestBrowserJourney drives the real browser end to end. It skips when no
// system browser is installed so the ordinary suite stays deterministic.
func TestBrowserJourney(t *testing.T) {
	ctrl, err := New()
	if err != nil {
		t.Skipf("no browser available: %v", err)
	}
	t.Cleanup(func() { _ = ctrl.Close() })

	srv := httptest.NewServer(testApp())
	t.Cleanup(srv.Close)

	ctx := context.Background()
	const sid = "journey"

	state, err := ctrl.Navigate(ctx, sid, srv.URL+"/login")
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if !strings.HasSuffix(state.URL, "/login") {
		t.Errorf("login URL = %q, want it to end in /login", state.URL)
	}

	snap, err := ctrl.Snapshot(ctx, sid)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var userRef string
	for _, e := range snap.Elements {
		if e.Name == "Username" {
			userRef = e.Ref
		}
	}
	if userRef == "" {
		t.Fatalf("username field not found in snapshot: %+v", snap.Elements)
	}

	if _, err := ctrl.Type(ctx, sid, userRef, "ada"); err != nil {
		t.Fatalf("type: %v", err)
	}

	// Re-snapshot to reference the submit button, then click it.
	snap, err = ctrl.Snapshot(ctx, sid)
	if err != nil {
		t.Fatalf("snapshot 2: %v", err)
	}
	var submitRef string
	for _, e := range snap.Elements {
		if strings.Contains(strings.ToLower(e.Name), "sign in") || e.Role == "button" {
			submitRef = e.Ref
		}
	}
	if submitRef == "" {
		t.Fatalf("submit button not found: %+v", snap.Elements)
	}
	after, err := ctrl.Click(ctx, sid, submitRef)
	if err != nil {
		t.Fatalf("click submit: %v", err)
	}
	if !strings.Contains(after.URL, "/welcome") {
		t.Errorf("after submit URL = %q, want /welcome with the typed user", after.URL)
	}
	if !strings.Contains(after.URL, "ada") {
		t.Errorf("typed value did not reach the server: %q", after.URL)
	}

	// The old references are from before the navigation, so they are stale now.
	if _, err := ctrl.Click(ctx, sid, submitRef); err == nil {
		t.Error("reference should be stale after navigation")
	}

	// The login page logged a console error; it should have been captured.
	// Navigate back to read it from the same session.
	if _, err := ctrl.Navigate(ctx, sid, srv.URL+"/login"); err != nil {
		t.Fatalf("navigate back: %v", err)
	}
	errs, err := ctrl.ConsoleErrors(ctx, sid)
	if err != nil {
		t.Fatalf("console: %v", err)
	}
	if len(errs) == 0 {
		t.Error("expected the deliberate console error to be captured")
	}

	shot, err := ctrl.Screenshot(ctx, sid)
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	if len(shot.PNG) == 0 {
		t.Error("screenshot returned no bytes")
	}
}

// TestNavigateRejectsBadSchemeReal confirms the origin gate holds with a real
// controller (no browser process is started for a rejected scheme).
func TestNavigateRejectsBadSchemeReal(t *testing.T) {
	ctrl, err := New()
	if err != nil {
		t.Skipf("no browser available: %v", err)
	}
	t.Cleanup(func() { _ = ctrl.Close() })
	if _, err := ctrl.Navigate(context.Background(), "s", "file:///etc/hosts"); err == nil {
		t.Error("file: scheme should be rejected before any browser launch")
	}
}
