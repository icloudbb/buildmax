package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
)

// recordingTurnEngine keeps the turn it was given, so a test can see whose
// turn the handler decided it was.
type recordingTurnEngine struct{ turn convchannel.Turn }

func (e *recordingTurnEngine) Process(_ context.Context, _, _ string, turn convchannel.Turn) (conversation.ConversationResult, error) {
	e.turn = turn
	return conversation.ConversationResult{Runs: []conversation.SpawnedRun{{TaskID: "tk_1", RunID: "tr_1"}}}, nil
}

// The key is the only identity a webhook call has: the conversation and the turn
// both belong to its owner. A configured stand-in identity used to be recorded
// as the creator, which is not a user, so every call failed.
func TestWebhookTurnBelongsToTheKeyOwner(t *testing.T) {
	users := &mock.MockUserStore{}
	owner, err := users.CreateUser(context.Background(), "owner@example.com", "free")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	conversations := &mock.MockConversationStore{}
	engine := &recordingTurnEngine{}
	h := NewHandler(Config{
		JWTSecret:           boundarySecret,
		UserStore:           users,
		UserWebhookKeyStore: &mock.MockUserWebhookKeyStore{Keys: map[string]string{"whk-plain": owner.ID}},
		ConversationStore:   conversations,
		WebhookAdapter:      convchannel.NewWebhookAdapter("message"),
		WebhookEngine:       engine,
	})
	mux := http.NewServeMux()
	h.Register(mux)

	req := httptest.NewRequest("POST", "/api/webhook", strings.NewReader(`{"message":"summarize"}`))
	req.Header.Set("Authorization", "Bearer whk-plain")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("webhook got %d, want 202: %s", rec.Code, rec.Body.String())
	}
	if len(conversations.Conversations) != 1 {
		t.Fatalf("conversations = %+v, want one", conversations.Conversations)
	}
	if c := conversations.Conversations[0]; c.UserID != owner.ID || c.CreatedBy != owner.ID || c.Channel != convchannel.ChannelWebhook {
		t.Errorf("conversation = %+v, want a webhook conversation owned and created by %s", c, owner.ID)
	}
	if engine.turn.UserID != owner.ID {
		t.Errorf("turn user = %q, want the key owner %s", engine.turn.UserID, owner.ID)
	}
}
