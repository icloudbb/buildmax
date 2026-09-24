package agentapp

import (
	"context"
	"fmt"
	"time"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
)

// SessionContext is one open session: the writer that owns it, the branch
// reduced to a read model, and the runtime selections a turn needs.
//
// It is the committing context §14 describes. Every change that a resumed turn
// would have to see goes through a method here and reaches the journal before
// the method returns; nothing mutates the read model on its own. A caller
// therefore cannot change resumable state without committing it, because there
// is no exported field to change.
//
// A SessionContext with no writer is unpersisted: it accumulates in memory and
// commits nothing. That is what a throwaway session for an estimate or a
// one-shot run gets, and it is why every commit path checks for a nil writer
// rather than assuming one.
type SessionContext struct {
	writer session.Writer

	meta  session.Meta
	state session.State

	// items is every record this session has, in physical order — what was on
	// disk when it opened plus what has been committed since. It is kept
	// because a rewind has to re-derive the read model from a different branch,
	// and the reduced state alone cannot say what is on the branch it moves to.
	items []session.Item

	// messageIDs holds the journal item id behind each entry in state.Messages,
	// so a compaction counted in messages can name the item it covers. The two
	// slices are appended to together and are always the same length.
	messageIDs []string
	// closed makes releasing a session idempotent: an early close and a
	// deferred one must not fire the SessionEnd hook twice.
	closed bool

	head    string
	lastSeq uint64

	turnID        string
	selectedModel string
}

// NewSessionContext returns an unpersisted session, for a run whose history
// nobody will resume.
func NewSessionContext(defaultModel string) *SessionContext {
	return &SessionContext{
		meta:          session.NewMeta(session.NewID(), session.KindUser, time.Now()),
		selectedModel: defaultModel,
	}
}

// newReadOnlyContext wraps a loaded session that holds no writer.
//
// Its commit paths are no-ops, so anything that would change resumable state
// silently does nothing — which is why callers that intend to write must use
// the writer path instead. It exists for the read-only views that must keep
// working while a turn holds the session.
func newReadOnlyContext(loaded session.Loaded, defaultModel string) *SessionContext {
	s := &SessionContext{
		meta:          loaded.Meta,
		state:         loaded.State,
		items:         loaded.Items,
		head:          loaded.Head,
		selectedModel: defaultModel,
	}
	if loaded.Meta.SelectedModel != "" {
		s.selectedModel = loaded.Meta.SelectedModel
	}
	s.messageIDs = messageIDsOnBranch(loaded.Items, loaded.Head)
	return s
}

// newWriterContext wraps an open writer and the branch it found.
func newWriterContext(w session.Writer, defaultModel string) *SessionContext {
	loaded := w.Loaded()
	s := &SessionContext{
		writer:        w,
		meta:          loaded.Meta,
		state:         loaded.State,
		head:          loaded.Head,
		selectedModel: defaultModel,
	}
	if loaded.Meta.SelectedModel != "" {
		s.selectedModel = loaded.Meta.SelectedModel
	}
	s.items = loaded.Items
	for _, it := range loaded.Items {
		s.lastSeq = it.Seq
	}
	s.messageIDs = messageIDsOnBranch(loaded.Items, loaded.Head)
	return s
}

// messageIDsOnBranch is the journal item id behind each entry of the reduced
// state's Messages, positionally aligned with it.
//
// Ids come from the branch, not from every record on disk: an abandoned
// branch's messages are not in state.Messages, so counting those here would put
// the two slices out of step. Every context derives them the same way — a read
// model that skipped this looked correct until something asked it to name a
// message, and got an empty list instead of an answer.
func messageIDsOnBranch(items []session.Item, head string) []string {
	branch, err := session.Branch(items, head)
	if err != nil {
		return nil
	}
	ids := make([]string, 0, len(branch))
	for _, it := range branch {
		switch it.Payload.(type) {
		case session.MessageItem, session.ToolResult:
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// --- identity and presentation ---

func (s *SessionContext) ID() string           { return s.meta.ID }
func (s *SessionContext) Title() string        { return s.meta.Title }
func (s *SessionContext) CreatedAt() time.Time { return s.meta.CreatedAt }
func (s *SessionContext) Meta() session.Meta   { return s.meta }

// Persisted reports whether this session commits anything.
func (s *SessionContext) Persisted() bool { return s != nil && s.writer != nil }

// --- usage, carried in metadata rather than history ---

func (s *SessionContext) PromptTokens() int     { return s.meta.PromptTokens }
func (s *SessionContext) CompletionTokens() int { return s.meta.CompletionTokens }
func (s *SessionContext) CacheReadTokens() int  { return s.meta.CacheReadTokens }
func (s *SessionContext) CacheWriteTokens() int { return s.meta.CacheWriteTokens }
func (s *SessionContext) Cost() *llm.Cost       { return s.meta.Cost }
func (s *SessionContext) CostIncomplete() bool  { return s.meta.CostIncomplete }

// ModelName returns the selected model for this session, or fallback.
func (s *SessionContext) ModelName(fallback string) string {
	if s == nil {
		return fallback
	}
	if s.selectedModel != "" {
		return s.selectedModel
	}
	return fallback
}

// SetModel updates the selected model for the next turn. It is a current
// selection, so it changes metadata and appends nothing to history.
func (s *SessionContext) SetModel(name string) {
	if s == nil {
		return
	}
	s.selectedModel = name
	s.meta.SelectedModel = name
}

// AdditionalPrompt is the extra system-prompt text this session runs under.
func (s *SessionContext) AdditionalPrompt() string { return s.state.AdditionalPrompt }

// SetAdditionalPrompt records the additional system prompt this turn resolved.
// It is durable state the next turn must see, so it commits.
func (s *SessionContext) SetAdditionalPrompt(text string) error {
	if s.state.AdditionalPrompt == text {
		// Resolved afresh every run and usually unchanged; committing an
		// identical value every turn would fill the journal with restatements.
		return nil
	}
	if err := s.commit(session.AdditionalPromptSet{Text: text}); err != nil {
		return err
	}
	s.state.AdditionalPrompt = text
	return nil
}

// --- agent.MessageHistory ---

// HistoryMessages returns the model-visible messages: the suffix after the
// compaction boundary.
func (s *SessionContext) HistoryMessages() []llm.Message { return s.state.HistoryMessages() }

// Messages returns the whole branch, compacted prefix included, for callers
// that summarise a session rather than send it to a model.
func (s *SessionContext) Messages() []llm.Message { return s.state.Messages }

// MessageIDs are the journal item ids behind Messages, positionally aligned, so
// a caller offering a rewind can name the item a message came from.
func (s *SessionContext) MessageIDs() []string { return s.messageIDs }

// Append commits one message and returns only once it is durable.
func (s *SessionContext) Append(m llm.Message) error {
	id, err := s.commitItem(session.MessageItem{Message: m})
	if err != nil {
		return err
	}
	s.state.Messages = append(s.state.Messages, m)
	s.messageIDs = append(s.messageIDs, id)
	return nil
}

// --- agent.ToolBoundaryHistory ---

// ToolExecutionStarted records that approved calls are about to enter their
// tools. It commits before returning, which is the whole point: a record that
// arrived after the tool would not distinguish anything.
func (s *SessionContext) ToolExecutionStarted(calls []agent.ToolCallStart) error {
	items := make([]session.Payload, 0, len(calls))
	for _, c := range calls {
		items = append(items, session.ToolExecutionStarted{ToolCallID: c.ID, ToolName: c.Name})
	}
	return s.commit(items...)
}

// AppendToolResult commits one call's outcome and projects it into the
// conversation as the tool-role message provider adapters expect.
func (s *SessionContext) AppendToolResult(out agent.ToolOutcome) error {
	id, err := s.commitItem(session.ToolResult{
		ToolCallID: out.ID,
		Status:     out.Status,
		Content:    out.Result,
		Parts:      out.Parts,
	})
	if err != nil {
		return err
	}
	s.state.Messages = append(s.state.Messages, llm.Message{
		Role:       "tool",
		ToolCallID: out.ID,
		Content:    out.Result,
		Parts:      out.Parts,
	})
	s.messageIDs = append(s.messageIDs, id)
	return nil
}

// --- agent.CompactionHistory ---

func (s *SessionContext) PriorSummary() string { return s.state.CompactionSummary }

// AddCompaction advances the compaction boundary and stores the summary.
//
// The record names the item it covers rather than a count, so the boundary
// stays meaningful on a branch: a count would be read against whatever messages
// a later reader happened to have.
func (s *SessionContext) AddCompaction(summary string, summarizedCount int) error {
	idx := s.state.CompactionIdx + summarizedCount
	if idx > len(s.messageIDs) {
		idx = len(s.messageIDs)
	}
	if idx == 0 {
		return fmt.Errorf("compaction covers no messages")
	}
	if err := s.commit(session.Compaction{
		CoveredHeadID: s.messageIDs[idx-1],
		Summary:       summary,
	}); err != nil {
		return err
	}
	s.state.CompactionIdx = idx
	s.state.CompactionSummary = summary
	return nil
}

// --- agent.NoteStore ---

func (s *SessionContext) Notes() []agent.Note { return s.state.Notes }

func (s *SessionContext) SetNotes(notes []agent.Note, iter int) error {
	stamped := agent.StampNotes(s.state.Notes, notes, iter)
	if err := s.commit(session.NotesReplaced{Notes: stamped}); err != nil {
		return err
	}
	s.state.Notes = stamped
	return nil
}

func (s *SessionContext) Todos() []agent.Todo { return s.state.Todos }

func (s *SessionContext) SetTodos(todos []agent.Todo, iter int) error {
	stamped := agent.StampTodos(s.state.Todos, todos, iter)
	if err := s.commit(session.TodosReplaced{Todos: stamped}); err != nil {
		return err
	}
	s.state.Todos = stamped
	return nil
}

// --- turn lifecycle ---

// BeginTurn opens a turn, recording the runtime identity it ran under. runID
// correlates the turn with its trace.
func (s *SessionContext) BeginTurn(runID, model, workspace string, contextWindow int, inputKind string) error {
	s.turnID = runID
	return s.commit(session.TurnStarted{
		RunID:         runID,
		Model:         model,
		WorkspaceRoot: workspace,
		ContextWindow: contextWindow,
		InputKind:     inputKind,
	})
}

// FinishTurn closes the turn with a terminal status. A turn closed here is not
// recovered on the next open, because a process that was alive to write this
// knew what it had done.
func (s *SessionContext) FinishTurn(status, errorClass string) error {
	if s.turnID == "" {
		return nil
	}
	err := s.commit(session.TurnFinished{Status: status, ErrorClass: errorClass})
	s.turnID = ""
	return err
}

// AddUsage folds one turn's usage and cost into the session's running totals.
// These live in metadata, so they do not touch the journal.
func (s *SessionContext) AddUsage(update session.MetaUpdate) {
	s.meta = session.ApplyMetaUpdate(s.meta, update, time.Now())
}

// BindDestination records where this session's prompts go, the first time a
// turn runs. It reports whether anything changed and so needs persisting.
func (s *SessionContext) BindDestination(dest string) bool {
	if s.meta.PromptDestination != "" {
		return false
	}
	s.meta.PromptDestination = dest
	s.meta.UpdatedAt = time.Now().UTC()
	return true
}

// SetTitle records a title. Presentation only, so metadata rather than history.
func (s *SessionContext) SetTitle(title string) {
	s.meta.Title = title
	s.meta.UpdatedAt = time.Now().UTC()
}

// SetWorkspace records where the next turn runs.
func (s *SessionContext) SetWorkspace(dir string) {
	s.meta.Workspace = dir
	s.meta.UpdatedAt = time.Now().UTC()
}

// AbandonedBy reports what happened after targetID, without changing anything.
//
// It exists so a surface can show the consequence before the user commits to
// it. A fork asks it directly: the span it names is what the copy will not
// know about. A rewind asks RewindPreview instead, because the message it is
// about to take back belongs to the span it removes.
func (s *SessionContext) AbandonedBy(targetID string) (session.AbandonedWork, error) {
	return session.Abandoned(s.items, s.head, targetID)
}

// RewindPreview reports what rewinding messageID would remove, without doing
// it.
//
// A choice made without knowing what it leaves behind is the thing §8.1 says
// rewind must not hide, so this answers the same question Rewind does, before
// the user commits to it rather than after.
func (s *SessionContext) RewindPreview(messageID string) (session.AbandonedWork, error) {
	landing, err := session.RewindLanding(s.items, s.head, messageID)
	if err != nil {
		return session.AbandonedWork{}, err
	}
	return session.Abandoned(s.items, s.head, landing)
}

// RewindOutcome is everything a surface needs after a rewind: what the move
// left in place, and what it has to hand back.
type RewindOutcome struct {
	// Abandoned is the work the conversation no longer mentions and the world
	// still has. It counts the rewound message itself, which left the branch
	// with everything after it.
	Abandoned session.AbandonedWork
	// Prompt is the text of the rewound message, for the surface to put back
	// in its input box. Taking a prompt away without returning it would make
	// rewind destructive of the one thing the user meant to keep.
	Prompt string
	// Attachments counts images the rewound message carried. Only the text
	// comes back, so a surface that showed the prompt returning has to say
	// these did not.
	Attachments int
}

// Rewind removes messageID and everything after it, and reports what it left
// in place.
//
// It is exclusive: the message a person picks is the prompt they want to edit
// and send again, so it leaves the branch rather than staying at the end of it,
// and comes back in the outcome. session.RewindLanding decides which record the
// head then names.
//
// The report is not optional decoration. Rewind returns the model's history to
// an earlier point and does not touch files, processes, or anything a network
// call reached (§8.1), so a caller that does not show the result is telling the
// user the opposite of what happened — and leaving the model to reason from a
// workspace picture that is no longer true. It is returned rather than logged
// for that reason: a caller has to receive it to ignore it.
//
// The report is computed before the append, because afterwards the branch it
// describes is no longer the live one.
func (s *SessionContext) Rewind(messageID string) (RewindOutcome, error) {
	if s.writer == nil {
		return RewindOutcome{}, fmt.Errorf("rewind: session is not persisted")
	}
	landing, err := session.RewindLanding(s.items, s.head, messageID)
	if err != nil {
		return RewindOutcome{}, err
	}
	abandoned, err := session.Abandoned(s.items, s.head, landing)
	if err != nil {
		return RewindOutcome{}, err
	}
	out := RewindOutcome{Abandoned: abandoned}
	if msg, ok := s.messageAt(messageID); ok {
		out.Prompt, out.Attachments = msg.Content, len(msg.Images())
	}
	// The head_selected record chains to the landing record, which is what
	// redirects the branch; the head stays "the last physical record" either
	// way.
	if err := s.commitTo(landing, session.HeadSelected{Reason: "user_rewind"}); err != nil {
		return RewindOutcome{}, err
	}
	if err := s.reduceFromHead(); err != nil {
		return RewindOutcome{}, err
	}
	return out, nil
}

// messageAt finds the branch message a journal item id names.
func (s *SessionContext) messageAt(itemID string) (llm.Message, bool) {
	for i, id := range s.messageIDs {
		if id == itemID && i < len(s.state.Messages) {
			return s.state.Messages[i], true
		}
	}
	return llm.Message{}, false
}

// adoptPrefix writes a forked prefix into a freshly created session and brings
// the read model up to it.
//
// It goes through the ordinary append path rather than writing the file
// directly, so a copied prefix is validated exactly as a lived one would be:
// the writer checks that each record continues the branch, and the journal
// syncs it. A fork that produced a journal the loader would later refuse is a
// worse outcome than a fork that fails now.
func (s *SessionContext) adoptPrefix(items []session.Item) error {
	if s.writer == nil {
		return fmt.Errorf("adopt prefix: session is not persisted")
	}
	if len(s.items) != 0 {
		return fmt.Errorf("adopt prefix: session already holds %d records", len(s.items))
	}
	if err := s.writer.Append(context.Background(), items...); err != nil {
		return err
	}
	s.items = append(s.items, items...)
	last := items[len(items)-1]
	s.head, s.lastSeq = last.ID, last.Seq
	return s.reduceFromHead()
}

// reduceFromHead rebuilds the read model from the branch the head now names.
//
// It re-reduces rather than unwinding what changed. Unwinding would need a
// second implementation of every rule the reducer already has — compaction
// boundaries, replaced note lists, the additional prompt — and the two would
// drift. Replaying is the same code path a fresh open takes.
func (s *SessionContext) reduceFromHead() error {
	state, err := session.Reduce(s.items, s.head)
	if err != nil {
		return err
	}
	s.state, s.messageIDs = state, messageIDsOnBranch(s.items, s.head)
	return nil
}

// Snapshot is the live session as a Loaded value, for callers that summarise
// it. It reads the in-memory state rather than the file: a turn commits as it
// runs, but metadata lands at the end, so re-reading the bundle mid-turn would
// answer about the turn before the one on screen.
func (s *SessionContext) Snapshot() session.Loaded {
	return session.Loaded{Meta: s.meta, Head: s.head, State: s.state}
}

// Recovery is what the last open found interrupted, or the zero value when it
// found nothing to repair.
func (s *SessionContext) Recovery() session.Recovery {
	if s.writer == nil {
		return session.Recovery{}
	}
	return s.writer.Loaded().Recovery
}

// Close releases the writer lock. Safe on an unpersisted session.
func (s *SessionContext) Close() error {
	if s == nil {
		return nil
	}
	s.closed = true
	if s.writer == nil {
		return nil
	}
	err := s.writer.Close()
	s.writer = nil
	return err
}

// Closed reports whether this session has already been released, so a caller
// that closes early can let a deferred close stand without it firing twice.
func (s *SessionContext) Closed() bool { return s != nil && s.closed }

// --- commit plumbing ---

// commit appends payloads as one durable batch, chained from the current head.
func (s *SessionContext) commit(payloads ...session.Payload) error {
	_, err := s.commitAll(payloads...)
	return err
}

// commitItem appends one payload and returns the id it was written under, for
// callers that have to refer back to it.
func (s *SessionContext) commitItem(payload session.Payload) (string, error) {
	ids, err := s.commitAll(payload)
	if err != nil {
		return "", err
	}
	if len(ids) == 0 {
		// Unpersisted: the read model still needs a distinct id so a later
		// compaction can name a covered message.
		return session.NewID(), nil
	}
	return ids[0], nil
}

// commitTo appends one payload chained to an explicit parent rather than to the
// current head. Only a rewind needs it: every other record continues the branch
// it is already on.
func (s *SessionContext) commitTo(parent string, payload session.Payload) error {
	if s.writer == nil {
		return nil
	}
	seq := s.lastSeq + 1
	item := session.NewItem(seq, session.NewID(), parent, time.Now(), s.turnID, payload)
	if err := s.writer.Append(context.Background(), item); err != nil {
		return err
	}
	s.items = append(s.items, item)
	s.head, s.lastSeq = item.ID, seq
	return nil
}

func (s *SessionContext) commitAll(payloads ...session.Payload) ([]string, error) {
	if len(payloads) == 0 {
		return nil, nil
	}
	if s.writer == nil {
		return nil, nil
	}
	items := make([]session.Item, 0, len(payloads))
	ids := make([]string, 0, len(payloads))
	parent, seq := s.head, s.lastSeq
	for _, p := range payloads {
		seq++
		id := session.NewID()
		items = append(items, session.NewItem(seq, id, parent, time.Now(), s.turnID, p))
		ids = append(ids, id)
		parent = id
	}
	if err := s.writer.Append(context.Background(), items...); err != nil {
		return nil, err
	}
	s.items = append(s.items, items...)
	s.head, s.lastSeq = parent, seq
	return ids, nil
}

// interface conformance, checked here so a missed method is a build failure
// rather than a silently skipped commit path.
var (
	_ agent.MessageHistory      = (*SessionContext)(nil)
	_ agent.CompactionHistory   = (*SessionContext)(nil)
	_ agent.NotesHistory        = (*SessionContext)(nil)
	_ agent.ToolBoundaryHistory = (*SessionContext)(nil)
)
