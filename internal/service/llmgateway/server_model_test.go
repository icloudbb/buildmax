package llmgateway_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
)

// spaceQuota records which space each check was asked about.
type spaceQuota struct {
	allow  bool
	spaces []string
}

func (q *spaceQuota) Check(_ context.Context, spaceID string, addRuns, addTokens int) (bool, string, error) {
	q.spaces = append(q.spaces, spaceID)
	if q.allow {
		return true, "", nil
	}
	return false, "quota exceeded: token limit", nil
}

func conversationClient(t *testing.T, svc *llmgateway.Service, targetID string) cllm.LLMClient {
	t.Helper()
	model := &llmgateway.ServerModel{Service: svc, TargetID: targetID}
	client, err := model.ForConversation(context.Background(), "u_member", "tm_team", "cv_one", "")
	if err != nil {
		t.Fatalf("ForConversation: %v", err)
	}
	return client
}

// A turn's call is the server's own inference on a target the deployment
// chose, so it is addressed by ID rather than resolved through the default, and
// the ledger names the turn: its person, its conversation, and the surface.
func TestServerModelRecordsTheTurnInTheLedger(t *testing.T) {
	client := &scriptedClient{
		content: "hi",
		deltas:  []string{"h", "i"},
		usage:   cllm.Usage{PromptTokens: 20, CompletionTokens: 2, TotalTokens: 22},
	}
	ledger := newFakeLedger()
	quota := &spaceQuota{allow: true}
	svc := serviceWith(t, client, ledger, quota)
	// With another model as the default, a call that fell back to name
	// resolution would record the wrong target.
	svc.Router.Resolver.DefaultModel = "Deep"

	bound := conversationClient(t, svc, "mt_fast")
	var streamed strings.Builder
	completion, err := bound.ChatCompletionStreaming(context.Background(),
		cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hello"}}},
		func(d string) { streamed.WriteString(d) })
	if err != nil {
		t.Fatalf("ChatCompletionStreaming: %v", err)
	}
	if completion.Content != "hi" || streamed.String() != "hi" || completion.Usage.TotalTokens != 22 {
		t.Errorf("completion = %+v, streamed %q", completion, streamed.String())
	}

	call, outcome := ledger.only(t)
	if call.Surface != coregw.CallSurfaceConversation {
		t.Errorf("surface = %q, want %q", call.Surface, coregw.CallSurfaceConversation)
	}
	if call.UserID == nil || *call.UserID != "u_member" {
		t.Errorf("user = %v, want u_member", call.UserID)
	}
	if call.ConversationID == nil || *call.ConversationID != "cv_one" {
		t.Errorf("conversation = %v, want cv_one", call.ConversationID)
	}
	if call.TaskRunID != nil || call.TaskID != nil {
		t.Error("a conversation call was attributed to a run")
	}
	if call.TargetID != "mt_fast" || call.Model != "Fast" {
		t.Errorf("model = %q (%q), want the chosen target under its catalog name", call.Model, call.TargetID)
	}
	if !call.Streaming {
		t.Error("a streamed turn was recorded as blocking")
	}
	if outcome.Usage == nil || outcome.Usage.TotalTokens != 22 {
		t.Errorf("outcome usage = %+v", outcome.Usage)
	}
	// Metered against the conversation's space, like a worker call against its
	// run's.
	if len(quota.spaces) != 1 || quota.spaces[0] != "tm_team" {
		t.Errorf("quota checked for %v, want [tm_team]", quota.spaces)
	}
	// The provider still sees the server, not a gateway-forwarded client.
	if want := (cllm.CallOrigin{Surface: coregw.CallSurfaceServer}); client.gotOrigin != want {
		t.Errorf("upstream origin = %+v, want %+v", client.gotOrigin, want)
	}
}

// A space over its token limit stops answering in chat the way it stops
// starting runs, and the refusal is the one every conversation transport already
// turns into a 429 or a chat reply.
func TestServerModelRefusesAnOverQuotaSpace(t *testing.T) {
	client := &scriptedClient{content: "never"}
	ledger := newFakeLedger()
	svc := serviceWith(t, client, ledger, &spaceQuota{allow: false})

	_, err := conversationClient(t, svc, "mt_fast").ChatCompletionBlocking(context.Background(),
		cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hello"}}})
	var public *apierr.Error
	if !errors.As(err, &public) || public.Kind() != apierr.KindQuotaExceeded {
		t.Fatalf("err = %v, want a quota refusal", err)
	}
	if !strings.Contains(err.Error(), "token limit") {
		t.Errorf("err = %q, want the quota's reason", err)
	}
	if client.gotMessages != nil {
		t.Error("the upstream was called for a refused space")
	}
	if len(ledger.opened) != 0 {
		t.Error("a refused call opened a ledger row")
	}
}

// A provider failure keeps its chain, so the loop can still tell what went
// wrong, and it is recorded as failed.
func TestServerModelKeepsTheProviderFailure(t *testing.T) {
	ledger := newFakeLedger()
	svc := serviceWith(t, &scriptedClient{err: cllm.ErrProviderRateLimited}, ledger, nil)

	_, err := conversationClient(t, svc, "mt_fast").ChatCompletionBlocking(context.Background(),
		cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hello"}}})
	if !errors.Is(err, cllm.ErrProviderRateLimited) {
		t.Fatalf("err = %v, want the provider's rate limit", err)
	}
	if _, outcome := ledger.only(t); outcome.Status != coregw.CallStatusFailed {
		t.Errorf("outcome = %q, want %q", outcome.Status, coregw.CallStatusFailed)
	}
}

// A target that cannot serve a turn fails when the turn binds, before anything
// is written, and reports the context window of the target it will call.
func TestServerModelResolvesTheTargetWhenATurnBinds(t *testing.T) {
	svc := serviceWith(t, &scriptedClient{}, newFakeLedger(), nil)

	for _, targetID := range []string{"mt_retired", "mt_deep", "mt_missing"} {
		model := &llmgateway.ServerModel{Service: svc, TargetID: targetID}
		if _, err := model.ForConversation(context.Background(), "u", "tm", "cv", ""); err == nil {
			t.Errorf("%s: bound a turn to a target that cannot serve it", targetID)
		}
	}

	bound := conversationClient(t, svc, "mt_fast")
	if bound.ContextWindow() == 0 {
		t.Error("the bound client reports no context window")
	}
}
