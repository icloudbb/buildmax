package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// recordingCheckpointer captures what it was handed and optionally writes a note, standing in
// for the model turn that agentapp.NoteCheckpointer performs.
type recordingCheckpointer struct {
	calls     [][]llm.Message
	iters     []int
	writeNote string
	store     NoteStore
	err       error
}

func (c *recordingCheckpointer) Checkpoint(ctx context.Context, discarded []llm.Message) error {
	c.calls = append(c.calls, discarded)
	c.iters = append(c.iters, IterationFromContext(ctx))
	if c.err != nil {
		return c.err
	}
	if c.writeNote != "" && c.store != nil {
		_ = c.store.SetNotes(append(c.store.Notes(), Note{Text: c.writeNote}), IterationFromContext(ctx))
	}
	return nil
}

// orderedCompactor records whether the checkpoint had already run when it was called.
type orderedCompactor struct {
	notesAtCompact [][]Note
	store          NoteStore
}

func (c *orderedCompactor) Compact(ctx context.Context, msgs []llm.Message) (string, llm.Usage, error) {
	if c.store != nil {
		c.notesAtCompact = append(c.notesAtCompact, append([]Note(nil), c.store.Notes()...))
	}
	return "summary", llm.Usage{}, nil
}

func runWithCheckpointer(t *testing.T, client llm.LLMClient, h MessageHistory, comp ContextCompactor, cp StateCheckpointer) {
	t.Helper()
	_, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      testMaxIter,
		History:      h,
		Compactor:    comp,
		Checkpointer: cp,
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
}

// TestCheckpoint_RunsBeforeCompactionWithDiscardedMessages pins the ordering the whole mechanism
// depends on: after Compact returns, the material is only reachable through a lossy summary, so
// the checkpoint has to see it first.
func TestCheckpoint_RunsBeforeCompactionWithDiscardedMessages(t *testing.T) {
	client := &windowedClient{window: testContextWindow}
	h := &statefulHistory{}
	cp := &recordingCheckpointer{writeNote: "saved at the checkpoint", store: h}
	comp := &orderedCompactor{store: h}
	fillToThreshold(&h.compactingHistory)

	runWithCheckpointer(t, client, h, comp, cp)

	if len(cp.calls) != 1 {
		t.Fatalf("checkpointer called %d times, want 1", len(cp.calls))
	}
	if len(cp.calls[0]) == 0 {
		t.Error("checkpointer was handed no messages")
	}
	if len(comp.notesAtCompact) != 1 {
		t.Fatalf("compactor called %d times, want 1", len(comp.notesAtCompact))
	}
	if len(comp.notesAtCompact[0]) != 1 {
		t.Error("the checkpoint had not run by the time the summarizer was called")
	}
}

// TestCheckpoint_ReceivesTheIteration asserts notes written at the checkpoint are stamped like
// any other, rather than landing with an unknown age.
func TestCheckpoint_ReceivesTheIteration(t *testing.T) {
	client := &windowedClient{window: testContextWindow}
	h := &statefulHistory{}
	cp := &recordingCheckpointer{}
	fillToThreshold(&h.compactingHistory)

	runWithCheckpointer(t, client, h, &factCompactor{}, cp)

	if len(cp.iters) != 1 || cp.iters[0] != 1 {
		t.Errorf("checkpoint saw iterations %v, want [1]", cp.iters)
	}
}

// TestCheckpoint_FailureDoesNotStopCompaction covers the fail-open rule: losing a checkpoint
// costs some context, but skipping the compaction it guards would cost the run.
func TestCheckpoint_FailureDoesNotStopCompaction(t *testing.T) {
	client := &windowedClient{window: testContextWindow}
	h := &statefulHistory{}
	cp := &recordingCheckpointer{err: errors.New("model unavailable")}
	comp := &factCompactor{}
	fillToThreshold(&h.compactingHistory)

	runWithCheckpointer(t, client, h, comp, cp)

	if len(comp.calls) != 1 {
		t.Fatalf("compactor called %d times, want 1 — a failed checkpoint blocked compaction", len(comp.calls))
	}
	if h.idx == 0 {
		t.Error("no compaction boundary was recorded")
	}
}

// TestCheckpoint_SkippedWhenHookBlocksCompaction asserts the checkpoint does not fire when there
// is going to be no compaction: it exists to save material that is about to be destroyed, and
// nothing is destroyed when the hook says no.
func TestCheckpoint_SkippedWhenHookBlocksCompaction(t *testing.T) {
	client := &windowedClient{window: testContextWindow}
	h := &statefulHistory{}
	cp := &recordingCheckpointer{}
	comp := &factCompactor{}
	fillToThreshold(&h.compactingHistory)

	_, _, _, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      testMaxIter,
		History:      h,
		Compactor:    comp,
		Checkpointer: cp,
		Hooks:        blockingHookRunner{event: HookPreCompact},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if len(cp.calls) != 0 {
		t.Errorf("checkpoint ran %d times despite compaction being blocked", len(cp.calls))
	}
}

// blockingHookRunner blocks one named event and allows everything else.
type blockingHookRunner struct{ event HookEvent }

func (r blockingHookRunner) Run(ctx context.Context, in HookInput) HookOutput {
	if in.Event == r.event {
		return HookOutput{Decision: HookDecisionBlock, Reason: "blocked by test"}
	}
	return HookOutput{}
}

// TestCheckpoint_NoteSurvivesTheMessagesThatProducedIt is the end-to-end claim of this phase: a
// fact stated only in the discarded window is still in front of the model afterwards, because
// the checkpoint moved it out of the message list first.
func TestCheckpoint_NoteSurvivesTheMessagesThatProducedIt(t *testing.T) {
	client := &windowedClient{window: testContextWindow}
	h := &statefulHistory{}
	cp := &recordingCheckpointer{writeNote: "the deadline is 14 days, stated once", store: h}
	fillToThreshold(&h.compactingHistory)

	runWithCheckpointer(t, client, h, &factCompactor{}, cp)

	if h.idx == 0 {
		t.Fatal("no compaction happened; the test proves nothing")
	}
	last := client.lastSent[len(client.lastSent)-1]
	if !strings.Contains(last.Content, "the deadline is 14 days") {
		t.Errorf("checkpointed note is not in front of the model; last message was %q", last.Content)
	}
}
