package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/core/agent"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/util"
)

// printOptions controls the behavior of runPrintMode. Built from CLI flags.
type printOptions struct {
	Prompt    string
	SessionID string
	// CreateIfMissing routes SessionID through OpenOrCreateSession instead of
	// the open-only OpenSession. Set only for an explicit --session-id; -r and
	// --continue leave it false so an unknown id still errors.
	CreateIfMissing bool
	ModelName       string
	Workspace       string
	Format          OutputFormat
	NoStream        bool
	Quiet           bool
	IncludeDeltas   bool
	// AdditionalSystemPrompt is this run's user-authored prompt text, appended as the system
	// prompt's last layer. Empty leaves a resumed session running under whatever text it
	// already had.
	AdditionalSystemPrompt string
	Overrides              runOverrides
}

// stdoutStreamSink writes each delta to stdout and flushes so output appears incrementally.
type stdoutStreamSink struct {
	w io.Writer
}

func (s *stdoutStreamSink) OnDelta(delta string) {
	_, _ = io.WriteString(s.w, delta)
	if f, ok := s.w.(*os.File); ok {
		_ = f.Sync()
	}
}

// jsonlEventSink writes each (filtered) event as one JSON line to stdout.
type jsonlEventSink struct {
	w             io.Writer
	mu            sync.Mutex
	includeDeltas bool
}

func (s *jsonlEventSink) OnEvent(ev agent.Event) {
	line, ok := eventToJSON(ev, s.includeDeltas)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.w.Write(line)
	_, _ = io.WriteString(s.w, "\n")
}

func runPrintMode(opts printOptions) error {
	source, err := resolveModelSource(context.Background())
	if err != nil {
		return printFatal(opts.Format, ExitModelError, err)
	}
	// stderr, so a --output json consumer still gets only the envelope on
	// stdout while the person running it still sees what crossed the boundary.
	if notice := issueSessionNotice(opts.Overrides.Issue, source); notice != "" {
		fmt.Fprint(os.Stderr, notice)
	}
	app, err := agentapp.NewAgentApp(printAppConfig(opts, source))
	if err != nil {
		return printFatal(opts.Format, ExitModelError, err)
	}
	defer app.Close()
	// Before the first turn, and on stderr: a run that quietly went without a
	// memory source, or that has just registered a duplicate Project for a
	// repository that moved, is the failure this reporting prevents. It stays
	// out of stdout so a piped result is still only the reply.
	for _, notice := range app.StartupNotices(relinkCommandHint) {
		fmt.Fprintln(os.Stderr, notice)
	}
	openSession := app.OpenSession
	if opts.CreateIfMissing {
		openSession = app.OpenOrCreateSession
	}
	sess, err := openSession(opts.SessionID)
	if err != nil {
		return printFatal(opts.Format, ExitModelError, err)
	}
	defer app.CloseSession(sess)
	if opts.ModelName != "" {
		sess.SetModel(opts.ModelName)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Choose stream sink: stream only in text mode, and only when --no-stream is not set.
	var streamSink cllm.StreamSink
	if opts.Format == OutputText && !opts.NoStream {
		streamSink = &stdoutStreamSink{w: os.Stdout}
	}

	// Track policy denials by observing events; also wire jsonl sink when needed.
	policyDenied := false
	eventSink := func(ev agent.Event) {
		if ev.Kind == agent.EventToolDenied && ev.DenyReason == agent.DenyReasonPolicy {
			policyDenied = true
		}
	}
	if opts.Format == OutputJSONL {
		jsonl := &jsonlEventSink{w: os.Stdout, includeDeltas: opts.IncludeDeltas}
		baseSink := eventSink
		eventSink = func(ev agent.Event) {
			baseSink(ev)
			jsonl.OnEvent(ev)
		}
	}

	out, runErr := app.RunPrompt(ctx, sess, opts.Prompt, agentapp.RunPromptOpts{Stream: streamSink, EventSink: eventSink})

	userCancelled := ctx.Err() != nil
	exitCode := classifyExit(runErr, policyDenied, userCancelled)

	switch opts.Format {
	case OutputJSON, OutputJSONL:
		emitResultEnvelope(os.Stdout, out, exitCode, runErr, policyDenied, opts.Format == OutputJSONL)
	default:
		emitTextSummary(os.Stdout, os.Stderr, out, runErr, opts)
	}

	if exitCode == ExitOK {
		return nil
	}
	return &ExitError{Code: exitCode, Err: runErr}
}

func printAppConfig(opts printOptions, source auth.ModelSource) agentapp.AppConfig {
	return agentapp.AppConfig{
		WorkspaceDir:           opts.Workspace,
		EnableMCP:              true,
		Policy:                 agent.AllowAllPolicy(),
		ModelEntries:           source.Entries,
		DefaultModel:           source.Default,
		ManagedServerURL:       source.ServerURL,
		ManagedToken:           auth.TokenForServer,
		ManagedTokenRenew:      auth.RenewRejected,
		ArtifactPublisher:      auth.ArtifactPublisherForSession(),
		Issue:                  issueContextOf(opts.Overrides.Issue),
		Surface:                coregw.CallSurfaceCLI,
		AdditionalSystemPrompt: opts.AdditionalSystemPrompt,
		SandboxRunOverride:     opts.Overrides.Sandbox,
		MaxIterations:          opts.Overrides.MaxIterations,
		EnableLocalProject:     true,
		DisableProjectMemory:   opts.Overrides.NoProjectMemory,
		// Local run under the user's own authority; the browser process lives
		// only for the synchronous run and is released on Close. See
		// docs/design/agent-browser-capability.md.
		EnableBrowser: true,
	}
}

func emitResultEnvelope(w io.Writer, out agentapp.RunResult, exitCode int, runErr error, policyDenied bool, jsonl bool) {
	env := buildResultEnvelope(out, exitCode, runErr, policyDenied, jsonl)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(env)
}

func emitTextSummary(stdout, stderr io.Writer, out agentapp.RunResult, runErr error, opts printOptions) {
	// If we streamed, terminate the reply line. If we didn't stream but have a reply (--no-stream), print it now.
	if opts.NoStream && len(out.Reply) > 0 {
		fmt.Fprintln(stdout, out.Reply)
	} else if !opts.NoStream && len(out.Reply) > 0 {
		fmt.Fprintln(stdout)
	}

	if runErr != nil {
		fmt.Fprintf(stderr, "error: %v\n", runErr)
	}
	if opts.Quiet {
		return
	}
	fmt.Fprintln(stdout, "---")
	fmt.Fprintf(stdout, "Session:    %s\n", out.SessionID)
	fmt.Fprintf(stdout, "Tool calls: %d\n", out.ToolCalls)
	fmt.Fprintf(stdout, "Duration:   %s\n", util.FormatDuration(out.Duration))
	fmt.Fprintf(stdout, "Workspace:  %s\n", out.Workspace)
	if out.PromptTokens > 0 || out.CompletionTokens > 0 || out.TotalPromptTokens > 0 || out.TotalCompletionTokens > 0 {
		fmt.Fprintf(stdout, "Tokens(in/out): %s\n", formatTokenUsageValue(out.PromptTokens, out.CompletionTokens, out.TotalPromptTokens, out.TotalCompletionTokens))
	}
	// Printed only when a provider reported cached tokens: a "0/0" line on a
	// provider that reports nothing would claim a miss nobody measured.
	if out.CacheReadTokens > 0 || out.CacheWriteTokens > 0 || out.TotalCacheReadTokens > 0 || out.TotalCacheWriteTokens > 0 {
		fmt.Fprintf(stdout, "Cache(read/write): %s\n", formatTokenUsageValue(out.CacheReadTokens, out.CacheWriteTokens, out.TotalCacheReadTokens, out.TotalCacheWriteTokens))
	}
	// Only where the model was priced. BuildMax does not know what a provider
	// charges, and a line reading "0.000000" would be a claim, not a silence.
	if line := formatSessionCost(out.Cost, out.CostIncomplete); line != "" {
		fmt.Fprintf(stdout, "Cost(session):  %s\n", line)
	}
}

// printFatal handles errors that occur before the agent has run (setup
// failures): emit an envelope in machine-readable formats, stderr otherwise.
func printFatal(format OutputFormat, code int, err error) error {
	switch format {
	case OutputJSON, OutputJSONL:
		env := printResult{
			ExitCode: code,
			Error:    &printErrorObj{Kind: errorKindForExitCode(code), Message: err.Error()},
		}
		if format == OutputJSONL {
			env.Type = "result"
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(env)
	default:
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
	}
	return &ExitError{Code: code, Err: err}
}

// formatSessionCost renders a session's estimated spend, or "" when there is
// nothing honest to say.
//
// A saving is shown only when caching actually saved. A run that wrote cache
// entries nothing read back paid more than it would have uncached, and calling
// that a small win would be the false claim this whole path avoids.
func formatSessionCost(cost *cllm.Cost, incomplete bool) string {
	if cost == nil {
		return ""
	}
	line := cllm.FormatAmount(cost.Total) + " " + cost.Currency
	if saved := cost.Saved(); saved > 0 {
		line += fmt.Sprintf(" (saved %s vs %s uncached)",
			cllm.FormatAmount(saved), cllm.FormatAmount(cost.Baseline))
	}
	if incomplete {
		line += " (partial: some calls were unpriced)"
	}
	return line
}
