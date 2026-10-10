package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"unicode/utf8"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util"
)

const eventApprovalRequest = "desktop/approval-request"

// ApprovalRequestPayload is emitted to the frontend when a tool call needs approval.
// SessionID routes it to the asking run's chat tab; ApprovalID is what that tab
// answers with, so concurrent sessions of one project never share a prompt.
type ApprovalRequestPayload struct {
	ApprovalID string         `json:"approval_id"`
	ProjectID  string         `json:"project_id"`
	SessionID  string         `json:"session_id"`
	ToolName   string         `json:"tool_name"`
	Args       map[string]any `json:"args"`
	// Target is what "Allow session" covers within the tool (an MCP
	// server/tool, a browser origin); empty when it covers every call.
	Target string `json:"target,omitempty"`
	// File is the current state of the file an Edit or Write would change, so
	// the prompt can show the change itself rather than its raw arguments. It
	// is read when the prompt is raised, from the root the tool writes under.
	File *ApprovalFile `json:"file,omitempty"`
}

// ApprovalFile is the file an Edit or Write approval would change, as it is
// now. Unavailable says why its content is not included; the frontend then
// shows the arguments alone.
type ApprovalFile struct {
	// Path is slash-separated and relative to the tool root when the file is
	// inside it.
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Content string `json:"content"`
	// Unavailable is one of approvalFileOutsideRoot, approvalFileBinary,
	// approvalFileTooLarge, approvalFileNotAFile, or approvalFileUnreadable.
	Unavailable string `json:"unavailable,omitempty"`
}

const (
	approvalFileOutsideRoot = "outside_root"
	approvalFileBinary      = "binary"
	approvalFileTooLarge    = "too_large"
	approvalFileNotAFile    = "not_a_file"
	approvalFileUnreadable  = "unreadable"
)

// approvalFile reads the file an Edit or Write call names, or returns nil for
// any other tool. It is confined like the file tools themselves, with symlinks
// followed as well: the prompt sends the bytes to the window, so a link inside
// the project must not show a file outside it. A partial file is never
// returned, since a diff against it would misstate what a Write replaces.
func approvalFile(root, toolName string, args map[string]any) *ApprovalFile {
	if toolName != tool.ToolNameEdit && toolName != tool.ToolNameWrite {
		return nil
	}
	requested, _ := args["file_path"].(string)
	if requested == "" {
		return nil
	}
	out := &ApprovalFile{Path: requested}
	if resolved, err := util.ResolvePath(root, requested); err == nil {
		if rel, err := filepath.Rel(root, resolved); err == nil {
			out.Path = filepath.ToSlash(rel)
		}
	}
	real, err := util.ResolveRealPath(root, requested)
	if errors.Is(err, util.ErrPathOutsideRoot) {
		out.Unavailable = approvalFileOutsideRoot
		return out
	}
	if err != nil {
		out.Unavailable = approvalFileUnreadable
		return out
	}
	info, err := os.Stat(real)
	if os.IsNotExist(err) {
		return out
	}
	if err != nil {
		out.Unavailable = approvalFileUnreadable
		return out
	}
	out.Exists = true
	if !info.Mode().IsRegular() {
		out.Unavailable = approvalFileNotAFile
		return out
	}
	if info.Size() > maxFilePreviewBytes {
		out.Unavailable = approvalFileTooLarge
		return out
	}
	f, err := os.Open(real)
	if err != nil {
		out.Unavailable = approvalFileUnreadable
		return out
	}
	defer f.Close()
	// Read one byte past the cap: the file may have grown since Stat.
	data, err := io.ReadAll(io.LimitReader(f, maxFilePreviewBytes+1))
	switch {
	case err != nil:
		out.Unavailable = approvalFileUnreadable
	case len(data) > maxFilePreviewBytes:
		out.Unavailable = approvalFileTooLarge
	case bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data):
		out.Unavailable = approvalFileBinary
	default:
		out.Content = string(data)
	}
	return out
}

// pendingAnswers holds the prompts awaiting an answer — tool approvals, or
// AskUser questions — keyed by a per-request id rather than by project or
// session: an id is answered at most once, so a late answer to a request its
// run already withdrew cannot resolve the next request that run makes.
type pendingAnswers[T any] struct {
	mu      sync.Mutex
	next    uint64
	waiting map[string]chan T
}

// open registers a new pending request and returns its id and answer channel.
func (p *pendingAnswers[T]) open() (string, chan T) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	id := strconv.FormatUint(p.next, 10)
	ch := make(chan T, 1)
	if p.waiting == nil {
		p.waiting = make(map[string]chan T)
	}
	p.waiting[id] = ch
	return id, ch
}

// withdraw drops a request its run stopped waiting for.
func (p *pendingAnswers[T]) withdraw(id string) {
	p.mu.Lock()
	delete(p.waiting, id)
	p.mu.Unlock()
}

// resolve answers one pending request. It fails for an id that is unknown,
// already answered, or withdrawn, so a stale answer reaches no run.
func (p *pendingAnswers[T]) resolve(id string, answer T) error {
	p.mu.Lock()
	ch, ok := p.waiting[id]
	delete(p.waiting, id)
	p.mu.Unlock()
	if !ok {
		return fmt.Errorf("no pending request %q: it was already answered or its run ended", id)
	}
	ch <- answer // buffered, and only one resolve can find it
	return nil
}

// runApprover is one run's agent.ApprovalHandler. It is bound to the run rather
// than the project so its prompt carries the run's own session id.
type runApprover struct {
	app *App
	run *desktopRun
}

// RequestApproval emits an approval-request event to the frontend and blocks until
// the user answers it via RespondApproval. Denies if the app context is not ready.
func (h *runApprover) RequestApproval(ctx context.Context, name string, args map[string]any, target string) agent.ApprovalDecision {
	h.app.mu.Lock()
	uiCtx := h.app.ctx // Wails context for emitting, distinct from the run's ctx
	h.app.mu.Unlock()
	if uiCtx == nil {
		return agent.ApprovalDeny
	}

	// The run's tools resolve paths against the session's workspace, which is
	// the root resolveWorkspace returns for it.
	var file *ApprovalFile
	if root, err := resolveWorkspace(h.run.projectID, h.run.sessionID); err == nil && root != "" {
		file = approvalFile(root, name, args)
	}

	id, answer := h.app.approvals.open()
	// A new chat's run has adopted its real id in OnStart, before any tool call,
	// so the prompt is routed to that chat tab and never to another new chat.
	h.app.emit(uiCtx, eventApprovalRequest, &ApprovalRequestPayload{
		ApprovalID: id,
		ProjectID:  h.run.projectID,
		SessionID:  h.run.sessionID,
		ToolName:   name,
		Args:       args,
		Target:     target,
		File:       file,
	})

	select {
	case d := <-answer:
		return d
	case <-ctx.Done():
		// Cancelled with the prompt still up. Without this the run goroutine
		// waits forever on an answer nobody will give, its deferred cleanup
		// never runs, and the session stays permanently "already in progress".
		h.app.approvals.withdraw(id)
		return agent.ApprovalDeny
	}
}
