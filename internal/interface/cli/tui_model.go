// Package cli provides the BuildMax CLI commands and interactive Bubble Tea UI.

package cli

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/agentapp/job"
	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/infra/git"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/util"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

const (
	carouselTick          = 400 // ms between carousel dot updates
	maxStreamPreviewLines = 12  // max lines of streaming buffer shown in live view
)

// toolSpinnerFrames cycles on each carousel tick to show progress while a tool is running.
var toolSpinnerFrames = [3]string{"⠋", "⠙", "⠸"}

// TUIOpts holds dependencies and display config for the TUI (agent, session, model name, paths).
type TUIOpts struct {
	App          *agentapp.AgentApp
	Session      *agentapp.SessionContext
	ModelName    string
	Workspace    util.Workspace
	SessionsDir  string
	Approval     agent.ApprovalHandler
	GlamourStyle string // "dark" or "light", detected once before the program starts
	RunStatus    agentapp.RunUsage
}

// agentDoneMsg is sent when the agent run finishes (in a tea.Cmd).
type agentDoneMsg struct {
	Result agentapp.RunResult
	Err    error
}

// streamDeltaMsg is sent when a content delta arrives from the streaming LLM.
type streamDeltaMsg struct {
	Delta string
}

// llmStartMsg is sent when a single LLM response starts inside an agent run.
type llmStartMsg struct{}

// llmEndMsg is sent when a single LLM response finishes inside an agent run.
type llmEndMsg struct {
	Content          string
	PromptTokens     int
	CompletionTokens int
	CacheReadTokens  int
	CacheWriteTokens int
}

type runStatusMsg struct {
	Status agentapp.RunUsage
}

// toolStartMsg is sent when the agent begins executing a tool call.
type toolStartMsg struct {
	CallID string
	Name   string
	Args   string
}

// toolEndMsg is sent when a tool call completes (success or error result).
type toolEndMsg struct {
	CallID   string
	Name     string
	Duration time.Duration
	IsError  bool
}

// toolDeniedMsg is sent when a tool call is blocked before execution.
type toolDeniedMsg struct {
	CallID string
	Name   string
	Reason string
}

// activeTool is one tool call in flight. Held as a slice keyed by call id
// rather than a single set of arguments: once several calls overlap, a result
// has to find its own arguments, and arrival order stops identifying them.
// Slice, not map, so the live view does not reorder between frames.
type activeTool struct {
	CallID string
	Name   string
	Args   string
}

// userInputInjectedMsg is sent when a queued message joins the running turn.
type userInputInjectedMsg struct {
	Text string
}

// userInputBlockedMsg is sent when a hook refuses a queued message.
type userInputBlockedMsg struct {
	Text   string
	Reason string
}

// carouselTickMsg is sent by tea.Tick to advance the assistant "..." carousel.
type carouselTickMsg struct{}

// assistantRenderedMsg is sent when the off-loop markdown rendering of a completed
// assistant reply is finished. Rendering runs in a goroutine Cmd to avoid blocking
// the Bubble Tea event loop.
type assistantRenderedMsg struct {
	line           string
	continueStream bool
}

// Model is the root Bubble Tea model in transcript mode.
// Chat history lives in the terminal scrollback; this model only manages the
// bottom live strip: streaming preview, input, slash panels, approval, and footer.
type Model struct {
	opts       TUIOpts
	inputBlock InputBlock
	busy       bool
	// busyLabel names the work in flight in the busy hint. Empty is a turn,
	// which is what "Generating" describes; a compaction is not one.
	busyLabel       string
	err             string // last error to show in footer
	width           int
	height          int
	carouselDots    int          // 0, 1, 2 for ".", "..", "..."
	focusInput      bool         // true = input has focus (slash panels may override)
	streamingBuffer string       // current LLM response text while streaming
	activeTools     []activeTool // tool calls in flight, shown in the live view
	streamChannel   chan tea.Msg // receives streamDeltaMsg, tool events, and agentDoneMsg
	slashMCP        *slashMCPState
	slashModel      *slashModelState
	slashSkills     *slashSkillsState
	slashSession    *slashSessionState
	slashHistory    *slashHistoryPointState
	slashDiff       *slashDiffState
	slashPopup      *slashPopupState
	// slashPopupInput is the input text the popup was last built from, and
	// slashPopupDismissed remembers that esc closed it for that same text.
	slashPopupInput     string
	slashPopupDismissed bool
	activePanel         slashPanel // generic panel (new abstraction); use openPanel/closeActivePanel
	branch              string
	userEmail           string
	pendingApproval     *approvalRequestMsg
	approvalSelected    int
	runStatus           agentapp.RunUsage
	// queue holds messages typed while a run was in flight. It is drained one
	// message per turn, after the run that was busy when they arrived finishes.
	queue *agent.MessageQueue
	// jobEvents delivers background-job lifecycle events; nil when the surface
	// has no job manager. jobEventsCancel releases the subscription on quit.
	jobEvents       <-chan job.Event
	jobEventsCancel func()
	// pendingRecap is a turn recap waiting for the assistant text it belongs
	// under. The normal path prints the reply before the run finishes, so the
	// recap can go straight to scrollback; the fallback render at agentDoneMsg
	// has not printed yet, and a recap above the answer it describes reads as
	// a recap of the wrong turn.
	pendingRecap string
	// parkedJobEvents are background events waiting to wake their owning
	// session, keyed by session ID: requested deliveries and react-monitor
	// lines. Only the session on screen drains — one event per turn, after
	// the user's own queued messages, because the user's words outrank a
	// job's news — and the rest wait for their session to come back.
	parkedJobEvents map[string][]agentapp.BackgroundEvent
	// runs owns prompt goroutines independently of Bubble Tea's command
	// goroutines, which the program stops waiting for as soon as it quits.
	runs *tuiRunOwner
}

// drainQueueMsg asks the model to start the next queued message, if any. It is a
// message rather than a direct call so the start of the next turn is ordered
// behind whatever the finished turn was still printing.
type drainQueueMsg struct{}

func drainQueueCmd() tea.Cmd {
	return func() tea.Msg { return drainQueueMsg{} }
}

// streamSinkToChannel implements agent.StreamSink by sending streamDeltaMsg to a channel.
type streamSinkToChannel struct {
	ctx     context.Context
	channel chan tea.Msg
}

func (s *streamSinkToChannel) OnDelta(delta string) {
	sendTUIMessage(s.ctx, s.channel, streamDeltaMsg{Delta: delta})
}

// eventSinkToChannel returns an agent.EventSink that forwards tool events to the stream channel.
func eventSinkToChannel(ctx context.Context, channel chan tea.Msg) func(agent.Event) {
	return func(e agent.Event) {
		switch e.Kind {
		case agent.EventLLMStart:
			if !sendTUIMessage(ctx, channel, runStatusMsg{Status: agentapp.RunUsage{
				ContextTokens:    e.ContextTokens,
				ContextWindow:    e.ContextWindow,
				PromptTokens:     e.PromptTokens,
				CompletionTokens: e.CompletionTokens,
				CacheReadTokens:  e.CacheReadTokens,
				CacheWriteTokens: e.CacheWriteTokens,
			}}) {
				return
			}
			sendTUIMessage(ctx, channel, llmStartMsg{})
		case agent.EventLLMEnd:
			sendTUIMessage(ctx, channel, llmEndMsg{
				Content:          e.Content,
				PromptTokens:     e.PromptTokens,
				CompletionTokens: e.CompletionTokens,
				CacheReadTokens:  e.CacheReadTokens,
				CacheWriteTokens: e.CacheWriteTokens,
			})
		case agent.EventToolStart:
			sendTUIMessage(ctx, channel, toolStartMsg{CallID: e.ToolCallID, Name: e.ToolName, Args: e.ToolArgs})
		case agent.EventToolEnd:
			sendTUIMessage(ctx, channel, toolEndMsg{CallID: e.ToolCallID, Name: e.ToolName, Duration: e.ToolDuration, IsError: strings.HasPrefix(e.ToolResult, "error:")})
		case agent.EventToolDenied:
			sendTUIMessage(ctx, channel, toolDeniedMsg{CallID: e.ToolCallID, Name: e.ToolName, Reason: e.DenyReason})
		case agent.EventUserInput:
			sendTUIMessage(ctx, channel, userInputInjectedMsg{Text: e.Content})
		case agent.EventUserInputBlocked:
			sendTUIMessage(ctx, channel, userInputBlockedMsg{Text: e.Content, Reason: e.DenyReason})
		}
	}
}

// CurrentSession is the session the model is holding now, which is not always
// the one it started with: /fork switches to the child it creates. Whoever
// releases the session at exit has to ask, rather than remember.
func (m *Model) CurrentSession() *agentapp.SessionContext {
	if m == nil {
		return nil
	}
	return m.opts.Session
}

// NewModel builds a TUI model with input block and stored opts.
func NewModel(opts TUIOpts) *Model {
	var userEmail string
	if info, err := auth.Info(); err == nil {
		userEmail = info.Email
	}
	m := &Model{
		opts:       opts,
		inputBlock: NewInputBlock(),
		width:      80,
		height:     24,
		focusInput: true,
		branch:     git.CurrentBranch(workspaceRoot(opts.Workspace)),
		userEmail:  userEmail,
		runStatus:  opts.RunStatus,
		queue:      agent.NewMessageQueue(agent.DefaultMaxQueuedMessages),
		runs:       newTUIRunOwner(context.Background()),
	}
	if opts.App != nil && opts.App.Jobs() != nil {
		m.jobEvents, m.jobEventsCancel = opts.App.Jobs().Subscribe("")
	}
	return m
}

// Close stops foreground runs and releases background-job subscriptions before
// the AgentApp they use is closed.
func (m *Model) Close() {
	if m == nil {
		return
	}
	if m.jobEventsCancel != nil {
		m.jobEventsCancel()
		m.jobEventsCancel = nil
	}
	if m.runs != nil {
		m.runs.Close()
	}
}

// Init runs when the program starts; focuses input and starts cursor blink.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{textarea.Blink, m.inputBlock.Focus()}
	if listen := listenJobsCmd(m.jobEvents); listen != nil {
		cmds = append(cmds, listen)
	}
	return tea.Batch(cmds...)
}

// FocusInput returns true when the input has focus (used by tests).
func (m *Model) FocusInput() bool {
	return m.focusInput
}

// runAgentWithStream starts the agent in a goroutine and returns a Cmd that reads the first event.
// queue is handed to the run so a message typed mid-run joins it at the next
// iteration boundary rather than waiting for the whole run to finish.
func runAgentWithStream(owner *tuiRunOwner, opts TUIOpts, text string, channel chan tea.Msg, queue *agent.MessageQueue) tea.Cmd {
	started := owner.Go(func(ctx context.Context) {
		defer close(channel)
		sink := &streamSinkToChannel{ctx: ctx, channel: channel}
		evSink := eventSinkToChannel(ctx, channel)
		result, err := opts.App.RunPrompt(ctx, opts.Session, text, agentapp.RunPromptOpts{
			Stream:    sink,
			Approval:  opts.Approval,
			EventSink: evSink,
			Pending:   queue,
			Digest:    true,
		})
		sendTUIMessage(ctx, channel, agentDoneMsg{Result: result, Err: err})
	})
	if !started {
		close(channel)
	}
	return func() tea.Msg { return <-channel }
}

func nextStreamMsgCmd(channel chan tea.Msg) tea.Cmd {
	if channel == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-channel
		if !ok {
			return nil
		}
		return msg
	}
}

func renderAssistantMsgCmd(text string, width int, style string, continueStream bool) tea.Cmd {
	return func() tea.Msg {
		return assistantRenderedMsg{line: formatAssistantMsgForScrollback(text, width, style), continueStream: continueStream}
	}
}

// renderStreamingPreview returns the last maxStreamPreviewLines of the streaming buffer
// for display in the live Bubble Tea view. Lines are truncated to terminal width so that
// Bubble Tea's cursor-movement math stays correct when the view shrinks after streaming ends.
func (m *Model) renderStreamingPreview() string {
	lines := strings.Split(m.streamingBuffer, "\n")
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxStreamPreviewLines {
		lines = lines[len(lines)-maxStreamPreviewLines:]
	}
	// Reserve space for the "• " / "  " prefix (2 runes) plus a small margin.
	maxW := m.width - 4
	if maxW < 10 {
		maxW = 10
	}
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}
		runes := []rune(line)
		if len(runes) > maxW {
			line = string(runes[:maxW-1]) + "…"
		}
		if i == 0 {
			b.WriteString(assistantGlyphStyle.Render("◆ ") + line)
		} else {
			b.WriteString("  " + line)
		}
	}
	return b.String()
}

func handleKeyMsg(m *Model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		m.runs.Cancel()
		return m, tea.Quit
	}
	// Approval prompt intercepts all keys when a tool call is waiting for approval.
	if m.pendingApproval != nil {
		switch msg.String() {
		case "left", "h":
			if m.approvalSelected > 0 {
				m.approvalSelected--
			}
		case "right", "l":
			if m.approvalSelected < len(approvalChoices)-1 {
				m.approvalSelected++
			}
		case "enter":
			m.answerApproval(approvalChoices[m.approvalSelected].decision)
		case "y", "Y":
			m.answerApproval(agent.ApprovalAllowOnce)
		case "a", "A":
			m.answerApproval(agent.ApprovalAllowSession)
		case "n", "N", "esc":
			m.answerApproval(agent.ApprovalDeny)
		}
		return m, nil
	}
	// Tab accepts the ghost suggestion. With nothing on offer it stays what it
	// was: a no-op, because transcript mode has no viewport to move focus to.
	if msg.Code == tea.KeyTab {
		if ghost := m.inputBlock.Ghost(); ghost != "" && !m.busy {
			m.inputBlock.SetValue(ghost)
			m.inputBlock.SetGhost("")
		}
		return m, nil
	}
	// Active slashPanel (new abstraction) gets first dibs on keys.
	if m.activePanel != nil && !m.busy {
		if handled, cmd := m.activePanel.HandleKey(m, msg); handled {
			return m, cmd
		}
	}
	if m.focusInput && m.slashPopup != nil && len(m.slashPopup.matches) > 0 {
		switch msg.Code {
		case tea.KeyUp:
			if m.slashPopup.selected > 0 {
				m.slashPopup.selected--
			} else {
				m.slashPopup.selected = len(m.slashPopup.matches) - 1
			}
			return m, nil
		case tea.KeyDown:
			m.slashPopup.selected = (m.slashPopup.selected + 1) % len(m.slashPopup.matches)
			return m, nil
		}
	}
	switch msg.Code {
	case tea.KeyEscape:
		if m.busy {
			return m, unqueueOrClearInput(m)
		}
		if m.slashPopup != nil {
			m.slashPopup = nil
			m.slashPopupDismissed = true
			return m, nil
		}
		m.inputBlock.Reset()
		m.inputBlock.SetGhost("")
		return m, nil
	case tea.KeyEnter:
		if m.busy {
			return m, queueMessage(m)
		}
		if m.slashPopup != nil && len(m.slashPopup.matches) > 0 {
			cmd := m.slashPopup.matches[m.slashPopup.selected]
			m.slashPopup = nil
			m.inputBlock.Reset()
			m.inputBlock.SyncHeight()
			return dispatchSlashCommand(m, cmd)
		}
		text := strings.TrimSpace(m.inputBlock.Value())
		if text == "" {
			return m, nil
		}
		if strings.HasPrefix(text, "/") {
			m.inputBlock.Reset()
			m.inputBlock.SyncHeight()
			m.slashPopup = nil
			parts := strings.Fields(text)
			if len(parts) == 0 {
				return m, nil
			}
			return dispatchSlashCommand(m, parts[0], parts[1:]...)
		}
		m.inputBlock.Reset()
		m.inputBlock.SyncHeight()
		return m, startRun(m, text)
	}
	// When the session panel is focused, route printable characters and backspace
	// to the search filter instead of the input block.
	cmd := m.inputBlock.Update(msg)
	m.inputBlock.SyncHeight()
	m.syncSlashPopupFromInput()
	return m, cmd
}

// beginRun puts the model into the running state and returns the channel the
// run will report on. Every path that starts a turn goes through it, so a
// surface-level thing that must not survive into the next turn — a stale
// suggestion, last turn's counters — is forgotten in one place.
func beginRun(m *Model) chan tea.Msg {
	m.busy = true
	m.busyLabel = ""
	m.err = ""
	m.carouselDots = 0
	m.streamingBuffer = ""
	// A suggestion predicts the answer to the question the last turn asked.
	// Once a turn is underway that question has been answered, whatever the
	// user actually sent.
	m.inputBlock.SetGhost("")
	m.runStatus.PromptTokens = 0
	m.runStatus.CompletionTokens = 0
	channel := make(chan tea.Msg)
	m.streamChannel = channel
	return channel
}

// dropStaleSuggestion forgets a predicted answer once the question it answers
// is no longer the last thing the conversation said. beginRun covers the move
// that runs a turn; this is for the ones that do not — resuming another
// session, rewinding this one, forking off it.
func (m *Model) dropStaleSuggestion() {
	m.inputBlock.SetGhost("")
}

// startRun begins an agent run for text and returns the Cmd that prints the user
// message and pumps the run's events. Both a fresh submission and a queued message
// go through here, so a queued turn is indistinguishable from one typed at the prompt.
func startRun(m *Model, text string) tea.Cmd {
	channel := beginRun(m)
	// tea.Println returns a Cmd in Bubble Tea v2; use Sequence so the user message
	// appears in scrollback before the agent starts reading from the channel.
	userLine := formatUserMsgForScrollback(text)
	return tea.Sequence(
		tea.Println(userLine+"\n"),
		tea.Batch(
			tea.Tick(time.Duration(carouselTick)*time.Millisecond, func(t time.Time) tea.Msg { return carouselTickMsg{} }),
			runAgentWithStream(m.runs, m.opts, text, channel, m.queue),
		),
	)
}

// queueMessage stores what the user typed during a run so it runs as its own turn
// once the current one finishes. Enter during a run used to be swallowed entirely.
func queueMessage(m *Model) tea.Cmd {
	text := strings.TrimSpace(m.inputBlock.Value())
	if text == "" {
		return nil
	}
	// Slash commands act on live TUI state (model switch, session load, panels), so
	// running one a turn later would apply it to a different world than the one the
	// user was looking at. Refuse rather than queue.
	if strings.HasPrefix(text, "/") {
		m.err = "slash commands are unavailable while the agent is running"
		return nil
	}
	pos, err := m.queue.Enqueue(text)
	if err != nil {
		m.err = fmt.Sprintf("%v (%d already waiting)", err, m.queue.Len())
		return nil
	}
	m.inputBlock.Reset()
	m.inputBlock.SyncHeight()
	m.slashPopup = nil
	m.err = ""
	return tea.Println(formatQueuedMsgForScrollback(text, pos) + "\n")
}

// unqueueOrClearInput is esc during a run: clear what is being typed, or take back
// the last queued message when there is nothing left to clear.
func unqueueOrClearInput(m *Model) tea.Cmd {
	if strings.TrimSpace(m.inputBlock.Value()) != "" {
		m.inputBlock.Reset()
		m.inputBlock.SyncHeight()
		m.slashPopup = nil
		return nil
	}
	text, ok := m.queue.DropLast()
	if !ok {
		return nil
	}
	return tea.Println(formatUnqueuedMsgForScrollback(text) + "\n")
}

// handleDrainQueue starts the next queued message. A run that failed still drains:
// the queue holds what the user asked for, and stranding it with no way to release
// it is worse than letting it fail on its own turn.
func handleDrainQueue(m *Model, _ drainQueueMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		return m, nil
	}
	text, ok := m.queue.Dequeue()
	if ok {
		return m, startRun(m, text)
	}
	if ev, ok := m.nextParkedJobEvent(); ok {
		return m, startBackgroundEventRun(m, ev)
	}
	return m, nil
}

func handleWindowSize(m *Model, msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	inputW := m.width - 4
	if inputW < 8 {
		inputW = 8
	}
	m.inputBlock.SetWidth(inputW)
	m.inputBlock.SyncHeight()
	m.syncSlashPopupFromInput()
	return m, nil
}

func handleAgentDone(m *Model, msg agentDoneMsg) (tea.Model, tea.Cmd) {
	text := m.streamingBuffer
	width := m.width
	glamourStyle := m.opts.GlamourStyle
	m.busy = false
	m.carouselDots = 0
	m.streamingBuffer = ""
	m.activeTools = nil
	m.streamChannel = nil
	if msg.Err != nil {
		m.err = msg.Err.Error()
		slog.Error("agent failed", "err", msg.Err)
		return m, drainQueueCmd()
	}
	m.runStatus = agentapp.RunUsage{
		ContextTokens:         msg.Result.ContextTokens,
		ContextWindow:         msg.Result.ContextWindow,
		PromptTokens:          msg.Result.PromptTokens,
		CompletionTokens:      msg.Result.CompletionTokens,
		TotalPromptTokens:     msg.Result.TotalPromptTokens,
		TotalCompletionTokens: msg.Result.TotalCompletionTokens,
		CacheReadTokens:       msg.Result.CacheReadTokens,
		CacheWriteTokens:      msg.Result.CacheWriteTokens,
		TotalCacheReadTokens:  msg.Result.TotalCacheReadTokens,
		TotalCacheWriteTokens: msg.Result.TotalCacheWriteTokens,
	}
	// Neither half of the digest is part of the conversation: the suggestion is
	// only offered while the input is empty, and the recap goes to scrollback
	// as a notice. Nothing here is sent back to the model.
	m.inputBlock.SetGhost(msg.Result.Digest.Suggestion)
	recap := formatRecapForScrollback(msg.Result.Digest.Recap, m.width)
	if strings.TrimSpace(text) == "" {
		if recap == "" {
			return m, drainQueueCmd()
		}
		return m, tea.Sequence(tea.Println(recap+"\n"), drainQueueCmd())
	}
	// Normally each LLM response is rendered on llmEndMsg. This fallback keeps a
	// successful run from dropping text if the stream closes without an LLM end event.
	m.pendingRecap = recap
	return m, tea.Sequence(renderAssistantMsgCmd(text, width, glamourStyle, false), drainQueueCmd())
}

func handleCarouselTick(m *Model, msg carouselTickMsg) (tea.Model, tea.Cmd) {
	if m.busy {
		m.carouselDots = (m.carouselDots + 1) % 3
		return m, tea.Tick(time.Duration(carouselTick)*time.Millisecond, func(t time.Time) tea.Msg { return carouselTickMsg{} })
	}
	return m, nil
}

func handleStreamDelta(m *Model, msg streamDeltaMsg) (tea.Model, tea.Cmd) {
	m.streamingBuffer += msg.Delta
	return m, nextStreamMsgCmd(m.streamChannel)
}

func handleLLMStart(m *Model, msg llmStartMsg) (tea.Model, tea.Cmd) {
	m.streamingBuffer = ""
	return m, nextStreamMsgCmd(m.streamChannel)
}

func handleLLMEnd(m *Model, msg llmEndMsg) (tea.Model, tea.Cmd) {
	m.runStatus = mergeRunStatus(m.runStatus, agentapp.RunUsage{
		PromptTokens:     msg.PromptTokens,
		CompletionTokens: msg.CompletionTokens,
		CacheReadTokens:  msg.CacheReadTokens,
		CacheWriteTokens: msg.CacheWriteTokens,
	})
	text := m.streamingBuffer
	if strings.TrimSpace(text) == "" {
		text = msg.Content
	}
	m.streamingBuffer = ""
	if strings.TrimSpace(text) == "" {
		return m, nextStreamMsgCmd(m.streamChannel)
	}
	return m, renderAssistantMsgCmd(text, m.width, m.opts.GlamourStyle, true)
}

func mergeRunStatus(prev, next agentapp.RunUsage) agentapp.RunUsage {
	if next.ContextTokens == 0 {
		next.ContextTokens = prev.ContextTokens
	}
	if next.ContextWindow == 0 {
		next.ContextWindow = prev.ContextWindow
	}
	if next.TotalPromptTokens == 0 {
		next.TotalPromptTokens = prev.TotalPromptTokens
		if next.PromptTokens > prev.PromptTokens {
			next.TotalPromptTokens += next.PromptTokens - prev.PromptTokens
		}
	}
	if next.TotalCompletionTokens == 0 {
		next.TotalCompletionTokens = prev.TotalCompletionTokens
		if next.CompletionTokens > prev.CompletionTokens {
			next.TotalCompletionTokens += next.CompletionTokens - prev.CompletionTokens
		}
	}
	if next.TotalCacheReadTokens == 0 {
		next.TotalCacheReadTokens = prev.TotalCacheReadTokens
		if next.CacheReadTokens > prev.CacheReadTokens {
			next.TotalCacheReadTokens += next.CacheReadTokens - prev.CacheReadTokens
		}
	}
	if next.TotalCacheWriteTokens == 0 {
		next.TotalCacheWriteTokens = prev.TotalCacheWriteTokens
		if next.CacheWriteTokens > prev.CacheWriteTokens {
			next.TotalCacheWriteTokens += next.CacheWriteTokens - prev.CacheWriteTokens
		}
	}
	return next
}

func handleRunStatus(m *Model, msg runStatusMsg) (tea.Model, tea.Cmd) {
	m.runStatus = mergeRunStatus(m.runStatus, msg.Status)
	return m, nextStreamMsgCmd(m.streamChannel)
}

func updateRunStatusContext(prev, next agentapp.RunUsage) agentapp.RunUsage {
	next.PromptTokens = prev.PromptTokens
	next.CompletionTokens = prev.CompletionTokens
	next.TotalPromptTokens = prev.TotalPromptTokens
	next.TotalCompletionTokens = prev.TotalCompletionTokens
	next.CacheReadTokens = prev.CacheReadTokens
	next.CacheWriteTokens = prev.CacheWriteTokens
	next.TotalCacheReadTokens = prev.TotalCacheReadTokens
	next.TotalCacheWriteTokens = prev.TotalCacheWriteTokens
	return next
}

// finishTool removes a completed call from the live view and renders its
// transcript line. It matches on call id and falls back to the oldest call of
// the same name, so a surface that ever emits an event without an id degrades
// to today's behaviour instead of leaking a spinner.
func (m *Model) finishTool(callID, name, glyph, suffix string) string {
	idx := -1
	for i, t := range m.activeTools {
		if callID != "" && t.CallID == callID {
			idx = i
			break
		}
		if callID == "" && t.Name == name && idx < 0 {
			idx = i
		}
	}
	var args string
	if idx >= 0 {
		args = shortArgs(m.activeTools[idx].Args)
		m.activeTools = append(m.activeTools[:idx], m.activeTools[idx+1:]...)
	}
	line := "  " + glyph + " " + toolDisplayName(name)
	if args != "" {
		line += " (" + args + ")"
	}
	return line + suffix
}

// label renders one in-flight call. The spinner glyph is added in View().
func (t activeTool) label() string {
	if args := shortArgs(t.Args); args != "" {
		return toolDisplayName(t.Name) + " (" + args + ")"
	}
	return toolDisplayName(t.Name)
}

func handleToolStart(m *Model, msg toolStartMsg) (tea.Model, tea.Cmd) {
	m.activeTools = append(m.activeTools, activeTool(msg))
	return m, nextStreamMsgCmd(m.streamChannel)
}

func handleToolEnd(m *Model, msg toolEndMsg) (tea.Model, tea.Cmd) {
	glyph := toolGlyphSuccessStyle.Render("•")
	if msg.IsError {
		glyph = toolGlyphFailStyle.Render("•")
	}
	line := m.finishTool(msg.CallID, msg.Name, glyph, "")
	return m, tea.Sequence(tea.Println(line+"\n"), nextStreamMsgCmd(m.streamChannel))
}

func handleToolDenied(m *Model, msg toolDeniedMsg) (tea.Model, tea.Cmd) {
	line := m.finishTool(msg.CallID, msg.Name, toolGlyphFailStyle.Render("•"), " [denied]")
	return m, tea.Sequence(tea.Println(line+"\n"), nextStreamMsgCmd(m.streamChannel))
}

// approvalChoices is the prompt's outcome set, left to right. Session grants
// are what keep a per-write prompt from becoming something users turn off.
var approvalChoices = []struct {
	label    string
	decision agent.ApprovalDecision
}{
	{"Allow once(y)", agent.ApprovalAllowOnce},
	{"Allow session(a)", agent.ApprovalAllowSession},
	{"Deny(n)", agent.ApprovalDeny},
}

// answerApproval resolves the waiting tool call and clears the prompt. It goes
// through the handler so the decision, the forward to any connected device, and
// the registry cleanup all happen in one race-safe place — the same path a
// remote answer takes.
func (m *Model) answerApproval(d agent.ApprovalDecision) {
	if m.pendingApproval == nil {
		return
	}
	if h, ok := m.opts.Approval.(*TUIApprovalHandler); ok {
		h.deliver(m.pendingApproval.id, d)
	} else {
		select {
		case m.pendingApproval.response <- d:
		default:
		}
	}
	m.pendingApproval = nil
	m.approvalSelected = 0
}

// handleApprovalResolved dismisses the local prompt when the request was answered
// elsewhere (a remote device, or a cancel).
func handleApprovalResolved(m *Model, msg approvalResolvedMsg) (tea.Model, tea.Cmd) {
	if m.pendingApproval != nil && m.pendingApproval.id == msg.id {
		m.pendingApproval = nil
		m.approvalSelected = 0
	}
	return m, nil
}

// renderApprovalPanel renders the tool-approval prompt when a tool call is waiting.
// handleUserInputInjected prints a queued message as sent, now that the run has
// picked it up. The dim "queued" line printed when it was typed stays above it:
// the pair reads as accepted, then sent.
func handleUserInputInjected(m *Model, msg userInputInjectedMsg) (tea.Model, tea.Cmd) {
	if strings.TrimSpace(msg.Text) == "" {
		return m, nextStreamMsgCmd(m.streamChannel)
	}
	line := formatUserMsgForScrollback(msg.Text)
	return m, tea.Sequence(tea.Println(line+"\n"), nextStreamMsgCmd(m.streamChannel))
}

func handleUserInputBlocked(m *Model, msg userInputBlockedMsg) (tea.Model, tea.Cmd) {
	line := formatBlockedMsgForScrollback(msg.Text, msg.Reason)
	return m, tea.Sequence(tea.Println(line+"\n"), nextStreamMsgCmd(m.streamChannel))
}

func (m *Model) renderApprovalPanel() string {
	if m.pendingApproval == nil {
		return ""
	}
	keys := make([]string, 0, len(m.pendingApproval.Args))
	for k := range m.pendingApproval.Args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var argLines []string
	for _, k := range keys {
		line := fmt.Sprintf("  %s: %v", k, m.pendingApproval.Args[k])
		if len(line) > m.width-6 {
			line = line[:m.width-9] + "..."
		}
		argLines = append(argLines, line)
	}

	buttons := make([]string, len(approvalChoices))
	for i, c := range approvalChoices {
		if i == m.approvalSelected {
			buttons[i] = approvalSelectedStyle.Render(c.label)
			continue
		}
		buttons[i] = approvalUnselectedStyle.Render(c.label)
	}

	body := fmt.Sprintf("Tool: %s\n%s\n\n%s    ←→ select  enter: confirm",
		m.pendingApproval.ToolName,
		strings.Join(argLines, "\n"),
		strings.Join(buttons, "  "),
	)
	return approvalPanelStyle.Width(m.width - 4).Render(body)
}

func (m *Model) renderInputView() string {
	boxWidth := m.width - 2
	if boxWidth < 10 {
		boxWidth = 10
	}
	return inputBoxStyle.Width(boxWidth).Render(m.inputBlock.View())
}

// renderBusyHint is the status line shown above the input during a run. The input
// itself stays on screen and editable, because enter now queues a message instead
// of being swallowed — typing has to echo for that to be usable.
func (m *Model) renderBusyHint() string {
	dots := []string{".", "..", "..."}
	label := m.busyLabel
	if label == "" {
		label = "Generating"
	}
	line := label + dots[m.carouselDots%3]
	if n := m.queue.Len(); n > 0 {
		line += fmt.Sprintf("  ·  %d queued", n)
	} else {
		line += "  ·  enter: queue message"
	}
	return toolGlyphPendingStyle.Render(line)
}

// modeTag says where this session's prompts go: straight to a provider from
// this machine, or through the deployment it is signed in to.
//
// It is the app's mode rather than the model's, so it is stated once and does
// not move when /model switches models. It is always shown: "where does this
// send my prompts" must never depend on a tag being absent.
func modeTag(serverURL string) string {
	if serverURL == "" {
		return "local"
	}
	return strings.TrimPrefix(strings.TrimPrefix(serverURL, "https://"), "http://")
}

// workspaceRoot reads a possibly-absent workspace. A Model built without one
// renders no path rather than panicking.
func workspaceRoot(ws util.Workspace) string {
	if ws == nil {
		return ""
	}
	return ws.Root()
}

func (m *Model) renderFooterView() string {
	// Read per render, not captured: the footer is how a user knows which tree
	// the agent is writing to, so it must not lag a move.
	workspacePart := "@" + workspaceRoot(m.opts.Workspace)
	if m.branch != "" {
		workspacePart += " (|-" + m.branch + ")"
	}
	currentModel := m.opts.ModelName
	if m.opts.Session != nil {
		currentModel = m.opts.Session.ModelName(currentModel)
	}
	modelPart := "model: " + currentModel + " (" + modeTag(m.opts.App.ManagedServerURL()) + ")"
	line1 := footerModelStyle.Render(modelPart) + " | " +
		footerBranchStyle.Render(workspacePart)
	if tag := sandboxFooterTag(m.opts.App); tag != "" {
		line1 += " | " + tag
	}
	if m.userEmail != "" {
		line1 += " | " + m.userEmail
	}

	line2 := formatRunStatus(m.runStatus) + " | ctrl+c: quit | esc: clear/dismiss | /… + ↑↓: commands"
	// Only while a suggestion is actually on offer: tab does nothing otherwise,
	// and a hint for a key that does nothing is worse than no hint.
	if m.inputBlock.Ghost() != "" && !m.busy {
		line2 += " | tab: accept suggestion"
	}
	if n := m.queue.Len(); n > 0 {
		line2 += fmt.Sprintf(" | queued: %d", n)
	}
	if m.activePanel != nil {
		if hint := m.activePanel.FooterHint(); hint != "" {
			line2 += " | " + hint
		}
	}
	if m.err != "" {
		line2 += " | error: " + m.err
	}
	return line1 + "\n" + line2
}

// formatRunStatus renders only the context share in the footer. The per-run
// token and cache breakdowns live in /info, which has room for them; the footer
// is scarce and keeps just the number a reader watches turn to turn.
func formatRunStatus(st agentapp.RunUsage) string {
	if st.ContextWindow == 0 {
		return "ctx: unknown"
	}
	percent := 0
	if st.ContextTokens > 0 {
		percent = int(float64(st.ContextTokens)/float64(st.ContextWindow)*100 + 0.5)
	}
	return fmt.Sprintf("ctx: %d%% (%s/%s)", percent, formatTokenCount(st.ContextTokens), formatTokenCount(st.ContextWindow))
}

func formatTokenUsageValue(promptTokens, completionTokens, totalPromptTokens, totalCompletionTokens int) string {
	usage := fmt.Sprintf("%s/%s", formatTokenCount(promptTokens), formatTokenCount(completionTokens))
	if totalPromptTokens > 0 || totalCompletionTokens > 0 {
		usage += fmt.Sprintf(" (%s/%s)", formatTokenCount(totalPromptTokens), formatTokenCount(totalCompletionTokens))
	}
	return usage
}

func formatTokenCount(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n%1000 == 0 {
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// Update handles messages: keys (quit, submit, focus toggle), resize, agent events.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return handleKeyMsg(m, msg)
	case tea.WindowSizeMsg:
		return handleWindowSize(m, msg)
	case agentDoneMsg:
		return handleAgentDone(m, msg)
	case compactDoneMsg:
		return handleCompactDone(m, msg)
	case llmStartMsg:
		return handleLLMStart(m, msg)
	case llmEndMsg:
		return handleLLMEnd(m, msg)
	case runStatusMsg:
		return handleRunStatus(m, msg)
	case streamDeltaMsg:
		return handleStreamDelta(m, msg)
	case toolStartMsg:
		return handleToolStart(m, msg)
	case toolEndMsg:
		return handleToolEnd(m, msg)
	case toolDeniedMsg:
		return handleToolDenied(m, msg)
	case userInputInjectedMsg:
		return handleUserInputInjected(m, msg)
	case userInputBlockedMsg:
		return handleUserInputBlocked(m, msg)
	case carouselTickMsg:
		return handleCarouselTick(m, msg)
	case drainQueueMsg:
		return handleDrainQueue(m, msg)
	case remotePromptMsg:
		return handleRemotePrompt(m, msg.text)
	case approvalResolvedMsg:
		return handleApprovalResolved(m, msg)
	case jobEventMsg:
		return handleJobEvent(m, msg)
	case approvalRequestMsg:
		m.pendingApproval = &msg
		m.approvalSelected = 0
		return m, nil
	case assistantRenderedMsg:
		if msg.continueStream {
			if msg.line == "" {
				return m, nextStreamMsgCmd(m.streamChannel)
			}
			return m, tea.Sequence(tea.Println(msg.line+"\n"), nextStreamMsgCmd(m.streamChannel))
		}
		// End of a turn: a recap held back for this render goes out under the
		// reply it describes, in the same write, so nothing can interleave.
		line := msg.line
		if m.pendingRecap != "" {
			if line != "" {
				line += "\n\n"
			}
			line += m.pendingRecap
			m.pendingRecap = ""
		}
		if line == "" {
			return m, nil
		}
		return m, tea.Println(line + "\n")
	default:
		// tea.Println returns a Cmd whose result is a tea-internal printLineMessage.
		// That message is handled by the renderer (insertAbove) but also reaches
		// model.Update. Forwarding it to the textarea causes the v2 bubbles viewport
		// to insert parts of the ANSI body as text via its default key handler.
		// Filter it out by type name before passing anything to the textarea.
		if fmt.Sprintf("%T", msg) == "tea.printLineMessage" {
			return m, nil
		}
		cmd := m.inputBlock.Update(msg)
		m.inputBlock.SyncHeight()
		m.syncSlashPopupFromInput()
		return m, cmd
	}
}

// View renders only the bottom live strip: streaming preview, slash panels, approval, input, footer.
// Chat history lives in the terminal scrollback and is not rendered here.
func (m *Model) View() tea.View {
	var parts []string

	// Streaming preview: show tail of buffer above the input while busy.
	if m.busy {
		if m.streamingBuffer != "" {
			parts = append(parts, m.renderStreamingPreview())
		}
		if len(m.activeTools) > 0 {
			spinner := toolGlyphPendingStyle.Render(toolSpinnerFrames[m.carouselDots])
			for _, t := range m.activeTools {
				parts = append(parts, "  "+spinner+" "+t.label())
			}
		}
		parts = append(parts, m.renderBusyHint())
	}

	// Slash panels (only shown when not busy).
	if !m.busy {
		if s := m.renderActivePanel(); s != "" {
			parts = append(parts, s)
		}
		if s := m.renderSlashPopupPanel(); s != "" {
			parts = append(parts, s)
		}
	}

	// Approval panel is shown regardless of busy state.
	if s := m.renderApprovalPanel(); s != "" {
		parts = append(parts, s)
	}

	parts = append(parts, m.renderInputView())
	parts = append(parts, m.renderFooterView())
	return tea.NewView(strings.Join(parts, "\n"))
}
