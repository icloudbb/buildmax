package tool

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
)

// BrowserController drives a real browser for one runtime. The implementation
// lives in internal/infra/browser; the tool layer depends only on this port so
// core and tool carry no CDP dependency and tests can substitute a fake.
//
// Every method is keyed by the run's session ID: one session never observes or
// drives another session's page. A call whose session has no page is an error,
// never a fall-through to some other page.
type BrowserController interface {
	Navigate(ctx context.Context, sessionID, rawURL string) (PageState, error)
	Snapshot(ctx context.Context, sessionID string) (PageSnapshot, error)
	Click(ctx context.Context, sessionID, ref string) (PageState, error)
	Type(ctx context.Context, sessionID, ref, text string) (PageState, error)
	Screenshot(ctx context.Context, sessionID string) (Screenshot, error)
	ConsoleErrors(ctx context.Context, sessionID string) ([]string, error)
}

// PageState is what every browser operation reports about the current page.
// Revision increments on navigation or DOM replacement; a snapshot's element
// references are valid only while Revision is unchanged.
type PageState struct {
	URL      string
	Title    string
	Status   int
	Viewport string
	Revision int
}

// Element is one interactive node in a snapshot, addressed by a reference that
// is stable only within its page revision.
type Element struct {
	Ref   string
	Role  string
	Name  string
	Value string
}

// PageSnapshot is a bounded, current view of the page for the model to act on.
type PageSnapshot struct {
	PageState
	Text     string
	Elements []Element
}

// Screenshot is a PNG capture of the current page plus its state.
type Screenshot struct {
	PageState
	PNG []byte
}

// errNoSession guards a browser tool called outside a session-scoped run; page
// ownership is keyed by session, so a call without one cannot be served.
var errNoSession = errors.New("browser tools require a session")

func sessionFor(ctx context.Context) (string, error) {
	id, ok := session.SessionIDFromContext(ctx)
	if !ok || id == "" {
		return "", errNoSession
	}
	return id, nil
}

// formatPageState renders a PageState for the model, meaningful on every path.
func formatPageState(s PageState) string {
	var b strings.Builder
	fmt.Fprintf(&b, "URL: %s\n", s.URL)
	fmt.Fprintf(&b, "Title: %s\n", s.Title)
	if s.Status > 0 {
		fmt.Fprintf(&b, "HTTP status: %d\n", s.Status)
	}
	if s.Viewport != "" {
		fmt.Fprintf(&b, "Viewport: %s\n", s.Viewport)
	}
	fmt.Fprintf(&b, "Revision: %d", s.Revision)
	return b.String()
}

// NewBrowserTools returns the focused browser tools bound to ctrl. It is only
// ever called where a controller exists; a nil controller means the surface
// registers no browser tools at all, per docs/design/agent-browser-capability.md.
func NewBrowserTools(ctrl BrowserController) []llm.Tool {
	if ctrl == nil {
		return nil
	}
	return []llm.Tool{
		&browserNavigate{ctrl},
		&browserSnapshot{ctrl},
		&browserClick{ctrl},
		&browserType{ctrl},
		&browserScreenshot{ctrl},
		&browserConsole{ctrl},
	}
}

// browserNavigate opens an approved HTTP(S) origin in the session's page.
type browserNavigate struct{ ctrl BrowserController }

func (*browserNavigate) Name() string                     { return ToolNameBrowserNavigate }
func (*browserNavigate) Access(map[string]any) llm.Access { return llm.AccessWrite }
func (*browserNavigate) Description() string {
	return "Open an HTTP(S) URL in the shared browser page and report the resulting URL, title, and HTTP status. Use to reach a page you then inspect and operate. Only http and https origins are allowed."
}
func (*browserNavigate) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type":        "string",
				"description": "The http(s) URL to open, e.g. http://localhost:3000/login",
			},
		},
		"required": []string{"url"},
	}
}
func (t *browserNavigate) Execute(ctx context.Context, args map[string]any) (string, error) {
	sessionID, err := sessionFor(ctx)
	if err != nil {
		return "", err
	}
	rawURL, err := parseRequiredString(args, "url")
	if err != nil {
		return "", err
	}
	state, err := t.ctrl.Navigate(ctx, sessionID, rawURL)
	if err != nil {
		return "", err
	}
	return formatPageState(state), nil
}

// browserSnapshot returns a bounded snapshot of the current page with element
// references the interaction tools accept.
type browserSnapshot struct{ ctrl BrowserController }

func (*browserSnapshot) Name() string                     { return ToolNameBrowserSnapshot }
func (*browserSnapshot) Access(map[string]any) llm.Access { return llm.AccessReadOnly }
func (*browserSnapshot) Description() string {
	return "Return a bounded snapshot of the current browser page: its interactive elements with stable references (use them with BrowserClick/BrowserType) plus visible text. References are valid only until the page navigates or its DOM is replaced."
}
func (*browserSnapshot) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *browserSnapshot) Execute(ctx context.Context, args map[string]any) (string, error) {
	sessionID, err := sessionFor(ctx)
	if err != nil {
		return "", err
	}
	snap, err := t.ctrl.Snapshot(ctx, sessionID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(formatPageState(snap.PageState))
	b.WriteString("\n\nElements:\n")
	if len(snap.Elements) == 0 {
		b.WriteString("(none)\n")
	}
	for _, e := range snap.Elements {
		fmt.Fprintf(&b, "[%s] %s", e.Ref, e.Role)
		if e.Name != "" {
			fmt.Fprintf(&b, " %q", e.Name)
		}
		if e.Value != "" {
			fmt.Fprintf(&b, " = %q", e.Value)
		}
		b.WriteByte('\n')
	}
	if snap.Text != "" {
		b.WriteString("\nText:\n")
		b.WriteString(snap.Text)
	}
	return b.String(), nil
}

// browserClick clicks a referenced element.
type browserClick struct{ ctrl BrowserController }

func (*browserClick) Name() string                     { return ToolNameBrowserClick }
func (*browserClick) Access(map[string]any) llm.Access { return llm.AccessWrite }
func (*browserClick) Description() string {
	return "Click an element in the current browser page by its reference from BrowserSnapshot. Returns the resulting page state, or an error if the reference is stale (take a fresh snapshot after navigation)."
}
func (*browserClick) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": "An element reference from the latest BrowserSnapshot, e.g. e3",
			},
		},
		"required": []string{"ref"},
	}
}
func (t *browserClick) Execute(ctx context.Context, args map[string]any) (string, error) {
	sessionID, err := sessionFor(ctx)
	if err != nil {
		return "", err
	}
	ref, err := parseRequiredString(args, "ref")
	if err != nil {
		return "", err
	}
	state, err := t.ctrl.Click(ctx, sessionID, ref)
	if err != nil {
		return "", err
	}
	return formatPageState(state), nil
}

// browserType types text into a referenced element.
type browserType struct{ ctrl BrowserController }

func (*browserType) Name() string                     { return ToolNameBrowserType }
func (*browserType) Access(map[string]any) llm.Access { return llm.AccessWrite }
func (*browserType) Description() string {
	return "Type text into an element in the current browser page by its reference from BrowserSnapshot. Returns the resulting page state, or an error if the reference is stale."
}
func (*browserType) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ref": map[string]any{
				"type":        "string",
				"description": "An element reference from the latest BrowserSnapshot, e.g. e2",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "The text to type into the element",
			},
		},
		"required": []string{"ref", "text"},
	}
}
func (t *browserType) Execute(ctx context.Context, args map[string]any) (string, error) {
	sessionID, err := sessionFor(ctx)
	if err != nil {
		return "", err
	}
	ref, err := parseRequiredString(args, "ref")
	if err != nil {
		return "", err
	}
	// Raw so a caller can type leading/trailing spaces or an empty field is
	// cleared explicitly by other means; the value itself is trusted content.
	text, ok := args["text"].(string)
	if !ok {
		return "", errors.New("text must be a string")
	}
	state, err := t.ctrl.Type(ctx, sessionID, ref, text)
	if err != nil {
		return "", err
	}
	return formatPageState(state), nil
}

// browserScreenshot captures the current page as a PNG image part.
type browserScreenshot struct{ ctrl BrowserController }

func (*browserScreenshot) Name() string                     { return ToolNameBrowserScreenshot }
func (*browserScreenshot) Access(map[string]any) llm.Access { return llm.AccessReadOnly }
func (*browserScreenshot) Description() string {
	return "Capture a screenshot of the current browser page. Use when visual judgment matters. Returns an image plus the page URL and title."
}
func (*browserScreenshot) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// Execute returns the text half alone for callers that cannot take an image.
func (t *browserScreenshot) Execute(ctx context.Context, args map[string]any) (string, error) {
	res, err := t.ExecuteMultimodal(ctx, args)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

// ExecuteMultimodal implements llm.MultimodalTool, attaching the PNG as an image
// part alongside a text line describing it, mirroring the MCP gateway's shape.
func (t *browserScreenshot) ExecuteMultimodal(ctx context.Context, args map[string]any) (llm.ToolResult, error) {
	sessionID, err := sessionFor(ctx)
	if err != nil {
		return llm.ToolResult{}, err
	}
	shot, err := t.ctrl.Screenshot(ctx, sessionID)
	if err != nil {
		return llm.ToolResult{}, err
	}
	text := "Screenshot of the current page.\n" + formatPageState(shot.PageState)
	return llm.ToolResult{
		Text: text,
		Parts: []llm.ContentPart{{
			Type:      llm.ContentPartImage,
			MediaType: "image/png",
			Data:      base64.StdEncoding.EncodeToString(shot.PNG),
		}},
	}, nil
}

// browserConsole reports recent console errors from the current page.
type browserConsole struct{ ctrl BrowserController }

func (*browserConsole) Name() string                     { return ToolNameBrowserConsole }
func (*browserConsole) Access(map[string]any) llm.Access { return llm.AccessReadOnly }
func (*browserConsole) Description() string {
	return "Return console errors seen since the current page was opened with BrowserNavigate. Use to check whether a page reports JavaScript errors. Returns a bounded excerpt."
}
func (*browserConsole) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *browserConsole) Execute(ctx context.Context, args map[string]any) (string, error) {
	sessionID, err := sessionFor(ctx)
	if err != nil {
		return "", err
	}
	errs, err := t.ctrl.ConsoleErrors(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if len(errs) == 0 {
		return "No console errors on the current page.", nil
	}
	return "Console errors:\n" + strings.Join(errs, "\n"), nil
}
