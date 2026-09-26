package tool

import (
	"context"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
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

// navRecorder is a stubBrowser that records the URLs it was asked to open.
type navRecorder struct {
	stubBrowser
	opened []string
}

func (r *navRecorder) Navigate(_ context.Context, _, rawURL string) (PageState, error) {
	r.opened = append(r.opened, rawURL)
	return PageState{URL: rawURL}, nil
}

// scriptedLLM issues one tool call per turn, then a final reply.
type scriptedLLM struct{ calls []llm.ToolCall }

func (s *scriptedLLM) ChatCompletionBlocking(context.Context, llm.Request) (llm.Completion, error) {
	if len(s.calls) == 0 {
		return llm.Completion{Content: "done"}, nil
	}
	c := s.calls[0]
	s.calls = s.calls[1:]
	return llm.Completion{ToolCalls: []llm.ToolCall{c}}, nil
}

func (s *scriptedLLM) ChatCompletionStreaming(ctx context.Context, req llm.Request, _ func(string)) (llm.Completion, error) {
	return s.ChatCompletionBlocking(ctx, req)
}

func (*scriptedLLM) ContextWindow() int { return 0 }

type memHistory struct{ msgs []llm.Message }

func (h *memHistory) HistoryMessages() []llm.Message { return h.msgs }
func (h *memHistory) Append(m llm.Message) error {
	h.msgs = append(h.msgs, m)
	return nil
}

// sessionApproval answers every prompt "allow for session" and records the
// grant target each prompt showed.
type sessionApproval struct{ targets []string }

func (a *sessionApproval) RequestApproval(_ context.Context, _ string, _ map[string]any, target string) agent.ApprovalDecision {
	a.targets = append(a.targets, target)
	return agent.ApprovalAllowSession
}

// TestBrowserNavigateSessionGrantCoversOneOrigin drives the real agent loop: an
// "allow for session" on one origin stops prompts for that origin — however its
// URL is spelled — and never admits another.
func TestBrowserNavigateSessionGrantCoversOneOrigin(t *testing.T) {
	nav := func(id, u string) llm.ToolCall {
		return llm.ToolCall{ID: id, Name: ToolNameBrowserNavigate, Arguments: `{"url":"` + u + `"}`}
	}
	client := &scriptedLLM{calls: []llm.ToolCall{
		nav("1", "http://localhost:3000/login"),
		nav("2", "HTTP://LocalHost:3000/dashboard"),
		nav("3", "http://localhost:3001/"),
		nav("4", "https://evil.example/"),
		nav("5", "http://localhost:3001/again"),
	}}
	ctrl := &navRecorder{}
	reg := llm.NewToolRegistry()
	reg.AppendTools(NewBrowserTools(ctrl)...)
	approval := &sessionApproval{}

	ctx := session.CtxWithSessionID(context.Background(), "s1")
	if _, _, _, err := agent.RunLoop(ctx, agent.RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: reg,
		History:      &memHistory{},
		MaxIter:      10,
		Approval:     approval,
		Grants:       agent.NewSessionGrants(),
	}); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if got := strings.Join(approval.targets, ","); got != "http://localhost:3000,http://localhost:3001,https://evil.example" {
		t.Errorf("prompted for %q; want one prompt per new origin, each naming it", got)
	}
	if len(ctrl.opened) != 5 {
		t.Errorf("opened %d pages, want 5 (every approved navigation runs)", len(ctrl.opened))
	}
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

// TestBrowserNavigateGrantScopeIsOneOrigin: an "allow for session" on
// BrowserNavigate covers exactly the origin of the URL it showed, normalized so
// the same origin is not re-asked and a different one always is.
func TestBrowserNavigateGrantScopeIsOneOrigin(t *testing.T) {
	var nav llm.GrantScoper = &browserNavigate{stubBrowser{}}
	scope := func(u string) string { return nav.GrantScope(map[string]any{"url": u}) }

	same := []string{
		"http://localhost:3000/login",
		"http://localhost:3000/other?q=1#x",
		"HTTP://LocalHost:3000/",
		"  http://localhost:3000  ",
	}
	for _, u := range same {
		if got := scope(u); got != "http://localhost:3000" {
			t.Errorf("GrantScope(%q) = %q, want http://localhost:3000", u, got)
		}
	}
	for u, want := range map[string]string{
		"http://example.com:80/a":    "http://example.com",
		"https://example.com:443/a":  "https://example.com",
		"https://example.com:80/a":   "https://example.com:80",
		"http://[::1]:8080/":         "http://[::1]:8080",
		"http://user:pw@example.com": "http://example.com",
	} {
		if got := scope(u); got != want {
			t.Errorf("GrantScope(%q) = %q, want %q", u, got, want)
		}
	}
	distinct := map[string]bool{}
	for _, u := range []string{
		"http://localhost:3000/", "http://localhost:3001/", "https://localhost:3000/",
		"http://127.0.0.1:3000/", "http://localhost.example.com:3000/",
	} {
		distinct[scope(u)] = true
	}
	if len(distinct) != 5 {
		t.Errorf("origins differing in scheme, host, or port must scope apart: %v", distinct)
	}
	// Unusable URLs never widen to a tool-wide grant for a real origin; they
	// fall back to the tool name and fail in Execute.
	for _, u := range []string{"file:///etc/passwd", "javascript:alert(1)", "localhost:3000", ""} {
		if got := scope(u); got != "" {
			t.Errorf("GrantScope(%q) = %q, want empty", u, got)
		}
	}
}

// TestBrowserInteractionToolsHaveNoGrantScope: click and type are confined to
// the admitted origin by the controller, so their grant stays tool-wide.
func TestBrowserInteractionToolsHaveNoGrantScope(t *testing.T) {
	for _, tl := range NewBrowserTools(stubBrowser{}) {
		if _, ok := tl.(llm.GrantScoper); ok && tl.Name() != ToolNameBrowserNavigate {
			t.Errorf("%s implements GrantScoper; only BrowserNavigate admits origins", tl.Name())
		}
	}
}

// TestPageStateReportsLeavingTheAdmittedOrigin: when a click or redirect takes
// the page off the approved origin, the result says so and says what to do.
func TestPageStateReportsLeavingTheAdmittedOrigin(t *testing.T) {
	on := formatPageState(PageState{URL: "http://LOCALHOST:3000/next", AdmittedOrigin: "http://localhost:3000"})
	if strings.Contains(on, "Origin:") {
		t.Errorf("same-origin page reported as off-origin:\n%s", on)
	}
	off := formatPageState(PageState{URL: "https://evil.example/landing", AdmittedOrigin: "http://localhost:3000"})
	for _, want := range []string{"https://evil.example", "http://localhost:3000", ToolNameBrowserNavigate, "refuse"} {
		if !strings.Contains(off, want) {
			t.Errorf("off-origin result missing %q:\n%s", want, off)
		}
	}
	if s := formatPageState(PageState{URL: "https://evil.example/"}); strings.Contains(s, "Origin:") {
		t.Errorf("no admitted origin known, so no origin note expected:\n%s", s)
	}
}

// TestNewBrowserToolsNilControllerIsEmpty confirms the surface registers nothing
// without a controller, so a nil capability means absent tools, not failing ones.
func TestNewBrowserToolsNilControllerIsEmpty(t *testing.T) {
	if tools := NewBrowserTools(nil); tools != nil {
		t.Errorf("nil controller should yield no tools, got %d", len(tools))
	}
}
