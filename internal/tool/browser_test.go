package tool

import (
	"context"
	"strings"
	"testing"
)

// stubBrowser is a no-op BrowserController for wiring tests.
type stubBrowser struct{}

func (stubBrowser) Navigate(context.Context, string, string) (PageState, error) {
	return PageState{}, nil
}
func (stubBrowser) Snapshot(context.Context, string) (PageSnapshot, error) {
	return PageSnapshot{}, nil
}
func (stubBrowser) Click(context.Context, string, string) (PageState, error) {
	return PageState{}, nil
}
func (stubBrowser) Type(context.Context, string, string, string) (PageState, error) {
	return PageState{}, nil
}
func (stubBrowser) Screenshot(context.Context, string) (Screenshot, error) {
	return Screenshot{}, nil
}
func (stubBrowser) ConsoleErrors(context.Context, string) ([]string, error) {
	return nil, nil
}

// TestBrowserToolsExposeNoCodeExecution guards the trust boundary that page
// content stays data: the surface is exactly the six observe/interact tools and
// offers no primitive that runs page-authored code (eval/exec/script). A future
// raw-JavaScript tool would let an untrusted page become authority and must be
// a deliberate, separately-reviewed decision — not slip in unnoticed.
func TestBrowserToolsExposeNoCodeExecution(t *testing.T) {
	tools := NewBrowserTools(stubBrowser{})
	want := []string{
		ToolNameBrowserNavigate, ToolNameBrowserSnapshot, ToolNameBrowserClick,
		ToolNameBrowserType, ToolNameBrowserScreenshot, ToolNameBrowserConsole,
	}
	if len(tools) != len(want) {
		t.Fatalf("got %d browser tools, want %d", len(tools), len(want))
	}
	got := map[string]bool{}
	for _, tl := range tools {
		got[tl.Name()] = true
	}
	for _, n := range want {
		if !got[n] {
			t.Errorf("missing browser tool %q", n)
		}
	}
	for name := range got {
		l := strings.ToLower(name)
		if strings.Contains(l, "eval") || strings.Contains(l, "exec") ||
			strings.Contains(l, "script") || strings.Contains(l, "javascript") {
			t.Errorf("browser tool %q looks like a code-execution primitive; page content must stay data, not authority", name)
		}
	}
}

// TestNewBrowserToolsNilControllerIsEmpty confirms the surface registers nothing
// without a controller, so a nil capability means absent tools, not failing ones.
func TestNewBrowserToolsNilControllerIsEmpty(t *testing.T) {
	if tools := NewBrowserTools(nil); tools != nil {
		t.Errorf("nil controller should yield no tools, got %d", len(tools))
	}
}
