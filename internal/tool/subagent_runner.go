package tool

import (
	"context"
	"fmt"
	"log/slog"

	coreagent "github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
)

// defaultSubAgentMaxIter is the iteration cap for sub-agents. Lower than
// config.DefaultMaxIterations because sub-agents are scoped, bounded tasks, and
// deliberately not raised by the parent run's own override: widening a
// delegation because the caller wanted a longer main run is how one runaway
// sub-agent becomes fifty.
const defaultSubAgentMaxIter = 50

// SubAgentRunOpts configures one sub-agent invocation.
type SubAgentRunOpts struct {
	Tools        []llm.Tool
	SystemPrompt string
	Description  string
	MaxIter      int    // 0 = defaultSubAgentMaxIter
	Model        string // "" = use runner default client
}

// SubAgentRunner runs a sub-agent with the given options and prompt.
// It is implemented by agentapp and injected when building the Task tool so that
// tools do not depend on a concrete runner; tests can inject a mock.
type SubAgentRunner interface {
	RunSubAgent(ctx context.Context, opts SubAgentRunOpts, prompt string) (reply string, err error)
}

// SubAgentTrace receives the event stream for one subagent run. It is kept as
// a small interface so this package stays below infra: agentapp supplies the
// durable trace implementation without making the tool layer know where or
// how traces are stored.
type SubAgentTrace interface {
	Record(coreagent.Event)
	Close() error
}

// SubAgentTraceFactory opens a trace for a subagent run. sessionID is the
// session the trace is filed under — the parent's, so a subagent's diagnostics
// stay reachable from a session a person can still find. Returning nil disables
// only this trace; the subagent must still run.
type SubAgentTraceFactory func(ctx context.Context, sessionID string, opts SubAgentRunOpts) SubAgentTrace

// SubAgentSession is one subagent's own conversation: everything the loop needs
// to read and commit history, plus the identity and close it needs to be a
// durable session rather than a scratch buffer.
//
// It is an interface here for the same reason SubAgentTrace is: this package
// sits below infra, so it says what it needs and lets agentapp supply the
// durable implementation.
type SubAgentSession interface {
	coreagent.NotesHistory
	coreagent.CompactionHistory
	ID() string
	Close() error
}

// SubAgentSessionFactory creates the private session for one subagent run.
//
// §9 of the local session storage plan gives every subagent its own hidden
// bundle: it must never write into the parent's journal or durable state, and
// the parent's Task result stays the model-facing return path. A nil factory
// falls back to an in-memory session, which is what a test or an embedder with
// no store gets.
type SubAgentSessionFactory func(ctx context.Context, opts SubAgentRunOpts) (SubAgentSession, error)

type defaultSubAgentRunner struct {
	client         llm.LLMClient
	policy         coreagent.ToolPolicy
	hooks          coreagent.HookRunner
	modelResolver  func(string) (llm.LLMClient, error) // nil = always use client
	traceFactory   SubAgentTraceFactory
	sessionFactory SubAgentSessionFactory
	maxParallel    int // 0 = sequential, as RunLoop reads it
}

// SubAgentRunnerOption configures a SubAgentRunner.
type SubAgentRunnerOption func(*defaultSubAgentRunner)

// WithSubAgentHooks attaches a parent hook runner so subagent runs honor the same
// PreToolUse / PostToolUse / lifecycle hooks as the parent agent. Nil disables hooks.
func WithSubAgentHooks(h coreagent.HookRunner) SubAgentRunnerOption {
	return func(r *defaultSubAgentRunner) { r.hooks = h }
}

// WithSubAgentTraceFactory attaches durable trace creation at the assembly
// layer. A nil factory leaves subagent execution unchanged.
func WithSubAgentTraceFactory(factory SubAgentTraceFactory) SubAgentRunnerOption {
	return func(r *defaultSubAgentRunner) { r.traceFactory = factory }
}

// WithSubAgentMaxParallelTools gives sub-agent runs the same tool-scheduling
// limit as the parent. Without it a sub-agent runs every call sequentially,
// which loses the setting exactly where it pays best: an exploration agent
// batches reads and searches for a living.
func WithSubAgentMaxParallelTools(n int) SubAgentRunnerOption {
	return func(r *defaultSubAgentRunner) { r.maxParallel = n }
}

// NewDefaultSubAgentRunner returns a SubAgentRunner backed by the given LLM client.
// policy is inherited from the parent agent run (nil = AllowAll).
// modelResolver looks up an LLM client by model name for agent types that specify a model;
// nil means always use client regardless of the model field.
func NewDefaultSubAgentRunner(llmClient llm.LLMClient, policy coreagent.ToolPolicy, modelResolver func(string) (llm.LLMClient, error), opts ...SubAgentRunnerOption) (SubAgentRunner, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("subagent runner: LLM client must not be nil")
	}
	r := &defaultSubAgentRunner{
		client:        llmClient,
		policy:        policy,
		modelResolver: modelResolver,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r, nil
}

func (r *defaultSubAgentRunner) RunSubAgent(ctx context.Context, opts SubAgentRunOpts, prompt string) (string, error) {
	client := r.client
	if opts.Model != "" && r.modelResolver != nil {
		if c, err := r.modelResolver(opts.Model); err == nil {
			client = c
		} else {
			slog.Warn("subagent model not found, using default client", "model", opts.Model, "err", err)
		}
	}

	maxIter := opts.MaxIter
	if maxIter <= 0 {
		maxIter = defaultSubAgentMaxIter
	}

	// Captured before the context is repointed at the subagent's own session,
	// so the trace stays filed under a session a user can still reach: a
	// subagent's own bundle is hidden from the picker.
	parentSessionID, _ := session.SessionIDFromContext(ctx)

	sess, err := r.newSession(ctx, opts)
	if err != nil {
		return "", err
	}
	defer func() {
		if cerr := sess.Close(); cerr != nil {
			slog.Warn("closing the subagent session failed", "err", cerr)
		}
	}()
	registry := llm.NewToolRegistry()
	registry.AppendTools(opts.Tools...)
	if err := sess.Append(llm.Message{Role: "user", Content: prompt}); err != nil {
		return "", err
	}
	ctx = session.CtxWithSessionID(ctx, sess.ID())
	// Point durable state at the subagent's own session. The parent's store arrives on the
	// context, and leaving it in place would let a subagent overwrite the notes and task list
	// of the run that delegated to it.
	ctx = coreagent.CtxWithNoteStore(ctx, sess)
	// A delegate carries no project memory: neither the index nor the tools.
	// It is the highest-volume run in a session, so it would pay the resident
	// cost most often, and a parent that needs it to know something says so in
	// the delegated task, which is more precise than handing over a catalogue.
	// Both tools are already absent from its registry; dropping the store means
	// a tool added later, or a user-defined agent definition, cannot reach it
	// by accident either.
	ctx = coreagent.CtxWithoutMemoryStore(ctx)
	ctx = coreagent.CtxMarkSubagent(ctx)

	// Fire SubagentStart so audit hooks can correlate the subagent run with
	// its parent. The decision is ignored — SubagentStart is advisory.
	if r.hooks != nil {
		r.hooks.Run(ctx, coreagent.HookInput{
			Event:      coreagent.HookSubagentStart,
			SessionID:  sess.ID(),
			IsSubagent: true,
			AgentType:  opts.Description,
			Prompt:     prompt,
		})
	}

	traceSessionID := parentSessionID
	if traceSessionID == "" {
		traceSessionID = sess.ID()
	}
	var eventSink func(coreagent.Event)
	if r.traceFactory != nil {
		if trace := r.traceFactory(ctx, traceSessionID, opts); trace != nil {
			defer func() { _ = trace.Close() }()
			eventSink = trace.Record
		}
	}

	reply, stats, _, err := coreagent.RunLoop(ctx, coreagent.RunLoopOpts{
		LLMClient:        client,
		SystemPrompt:     opts.SystemPrompt,
		ToolRegistry:     registry,
		MaxIter:          maxIter,
		History:          sess,
		Policy:           r.policy,
		Hooks:            r.hooks,
		SessionID:        sess.ID(),
		IsSubagent:       true,
		AgentType:        opts.Description,
		EventSink:        eventSink,
		MaxParallelTools: r.maxParallel,
	})
	// Reported before the error check: a subagent that failed still spent the
	// tokens it spent, and a bill that drops failed work understates the run.
	// ctx here still carries the parent's accumulator — RunLoop installs the
	// subagent's own on a context that does not escape it.
	coreagent.DelegatedUsageFromCtx(ctx).Report(stats)
	if err != nil {
		return "", err
	}
	return reply, nil
}
