package work

// Fixtures copied rather than imported: a helper crossing a package boundary
// makes the test boundary softer than the code's.

import (
	"context"
	"time"

	"github.com/icloudbb/buildmax/internal/mock"

	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// llmStubLedger accepts every write and keeps the last one so a test can check
// what a call was attributed to.
type llmStubLedger struct {
	opened  int
	last    coregw.Call
	calls   []coregw.Call
	listErr error
}

func (l *llmStubLedger) OpenLLMCall(_ context.Context, call *coregw.Call) (*coregw.Call, error) {
	l.opened++
	stored := *call
	stored.ID = "lc_stub"
	l.last = stored
	return &stored, nil
}
func (l *llmStubLedger) CompleteLLMCall(context.Context, string, coregw.CallOutcome) error {
	return nil
}
func (l *llmStubLedger) GetLLMCall(context.Context, string) (*coregw.Call, error) { return nil, nil }

func (l *llmStubLedger) GetLLMCallByClientID(context.Context, string, string) (*coregw.Call, error) {
	return nil, nil
}
func (l *llmStubLedger) ListLLMCallsByTaskRun(_ context.Context, taskRunID string) ([]coregw.Call, error) {
	if l.listErr != nil {
		return nil, l.listErr
	}
	var out []coregw.Call
	for _, call := range l.calls {
		if call.TaskRunID != nil && *call.TaskRunID == taskRunID {
			out = append(out, call)
		}
	}
	return out, nil
}
func (l *llmStubLedger) SearchLLMCalls(context.Context, coregw.CallFilter, int, int) ([]coregw.Call, int, error) {
	return nil, 0, nil
}

const llmTestSecret = "test-llm-secret"

func llmTestSpaceStore() *mock.MockSpaceStore {
	return &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: llmTestSpace, Name: "LLM Space", CreatedBy: llmTestUser, CreatedAt: time.Now().UTC()},
		},
		Members: []corespace.Member{
			{SpaceID: llmTestSpace, UserID: llmTestUser, Role: corespace.RoleOwner, CreatedAt: time.Now().UTC()},
		},
	}
}

const llmTestUser = "u_llm"

const llmTestSpace = "tm_llm"

func (l *llmStubLedger) SummarizeForegroundLLMCalls(context.Context, string, time.Time) ([]coregw.CallTotals, error) {
	return nil, nil
}
