package handlers

import (
	"context"
	"encoding/json"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/llm"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"

	gws "github.com/gorilla/websocket"
)

const wsTestSecret = "ws-test-secret"

func setupWSHandler() *Handler {
	spaceID := "tm_personal_u1"
	return NewHandler(Config{
		JWTSecret:         wsTestSecret,
		CORSOrigin:        "*",
		SpaceStore:        &mock.MockSpaceStore{Spaces: []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}}, Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}}},
		ConversationStore: &mock.MockConversationStore{},
	})
}

func dialWS(t *testing.T, server *httptest.Server, token string) *gws.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/spaces/tm_personal_u1/ws?token=" + token
	conn, resp, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v (resp=%v)", err, resp)
	}
	return conn
}

func readEnvelope(t *testing.T, conn *gws.Conn) wsconn.Envelope {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	env, err := wsconn.Decode(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env
}

func sendEnvelope(t *testing.T, conn *gws.Conn, typ string, payload any) {
	t.Helper()
	data, err := wsconn.Encode(typ, payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := conn.WriteMessage(gws.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestWSUpgradeRequiresToken(t *testing.T) {
	h := setupWSHandler()
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/spaces/tm_personal_u1/ws"
	_, resp, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWSUpgradeInvalidToken(t *testing.T) {
	h := setupWSHandler()
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/spaces/tm_personal_u1/ws?token=bad"
	_, resp, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

func TestWSConversationCreateFlow(t *testing.T) {
	h := setupWSHandler()
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	token := testsupport.SignJWT("u1", wsTestSecret)
	conn := dialWS(t, server, token)
	defer conn.Close()

	sendEnvelope(t, conn, wsconn.TypeConversationCreate, wsconn.ConversationCreate{
		Message: "hello",
	})

	env := readEnvelope(t, conn)
	if env.Type != wsconn.TypeConversationCreated {
		t.Fatalf("first event type = %q, want %q", env.Type, wsconn.TypeConversationCreated)
	}
	var created wsconn.ConversationCreated
	if err := json.Unmarshal(env.Payload, &created); err != nil {
		t.Fatal(err)
	}
	if created.ConversationID == "" {
		t.Error("conversation_id is empty")
	}

	// Since LLMClient is nil, we should get an error then completed
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		env = readEnvelope(t, conn)
		seen[env.Type] = true
		if env.Type == wsconn.TypeMessageCompleted {
			break
		}
	}
	if !seen[wsconn.TypeMessageCompleted] {
		t.Error("did not receive conversation.message.completed")
	}
}

// A socket may not name a channel the HTTP route refuses: system would strip
// the turn's tools, and a chat platform's channel belongs to the gateway.
func TestWSConversationCreateRefusesServerAssignedChannels(t *testing.T) {
	h := setupWSHandler()
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	conn := dialWS(t, server, testsupport.SignJWT("u1", wsTestSecret))
	defer conn.Close()
	for _, channel := range []string{"system", "telegram"} {
		sendEnvelope(t, conn, wsconn.TypeConversationCreate, wsconn.ConversationCreate{Message: "hi", Channel: channel})
		if env := readEnvelope(t, conn); env.Type != wsconn.TypeConversationError {
			t.Errorf("channel %q: event = %q, want %q", channel, env.Type, wsconn.TypeConversationError)
		}
	}
}

func TestWSUnknownEventType(t *testing.T) {
	h := setupWSHandler()
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	token := testsupport.SignJWT("u1", wsTestSecret)
	conn := dialWS(t, server, token)
	defer conn.Close()

	sendEnvelope(t, conn, "unknown.event", map[string]string{})

	env := readEnvelope(t, conn)
	if env.Type != wsconn.TypeSystemError {
		t.Errorf("type = %q, want %q", env.Type, wsconn.TypeSystemError)
	}
}

// gatedLLMClient blocks its first completion until release is closed, so a test can
// hold a conversation turn open and submit a second message while it runs.
type gatedLLMClient struct {
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func newGatedLLMClient() *gatedLLMClient {
	return &gatedLLMClient{started: make(chan struct{}), release: make(chan struct{})}
}

func (c *gatedLLMClient) gate() {
	first := false
	c.once.Do(func() { first = true })
	if !first {
		return
	}
	close(c.started)
	<-c.release
}

func (c *gatedLLMClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	c.gate()
	return llm.Completion{Content: "ok"}, nil
}

func (c *gatedLLMClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	c.gate()
	onDelta("ok")
	return llm.Completion{Content: "ok"}, nil
}

func (c *gatedLLMClient) ContextWindow() int { return 0 }

// A message sent while a turn is running is queued and then runs as its own turn.
// It used to come back as conversation.error and be dropped.
func TestWSConversationMessageQueuesWhileBusy(t *testing.T) {
	spaceID := "tm_personal_u1"
	client := newGatedLLMClient()
	h := NewHandler(Config{
		JWTSecret:                wsTestSecret,
		CORSOrigin:               "*",
		SpaceStore:               &mock.MockSpaceStore{Spaces: []corespace.Space{{ID: spaceID, Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}}, Members: []corespace.Member{{SpaceID: spaceID, UserID: "u1", Role: corespace.RoleOwner}}},
		ConversationStore:        &mock.MockConversationStore{},
		ConversationMessageStore: &mock.MockConversationMessageStore{},
		ConversationLLMClient:    client,
	})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	conn := dialWS(t, server, testsupport.SignJWT("u1", wsTestSecret))
	defer conn.Close()

	sendEnvelope(t, conn, wsconn.TypeConversationCreate, wsconn.ConversationCreate{Message: "first"})
	env := readEnvelope(t, conn)
	if env.Type != wsconn.TypeConversationCreated {
		t.Fatalf("first event = %q, want %q", env.Type, wsconn.TypeConversationCreated)
	}
	var created wsconn.ConversationCreated
	if err := json.Unmarshal(env.Payload, &created); err != nil {
		t.Fatal(err)
	}

	// The first turn is now parked inside the LLM call.
	<-client.started

	sendEnvelope(t, conn, wsconn.TypeConversationMessage, wsconn.ConversationMessage{
		ConversationID: created.ConversationID,
		Content:        "second",
	})

	var queued wsconn.MessageQueued
	for {
		env = readEnvelope(t, conn)
		if env.Type == wsconn.TypeConversationError {
			t.Fatalf("a message sent during a turn was rejected: %s", env.Payload)
		}
		if env.Type == wsconn.TypeMessageQueued {
			if err := json.Unmarshal(env.Payload, &queued); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if queued.Position != 1 {
		t.Errorf("queued position = %d, want 1", queued.Position)
	}
	if queued.Content != "second" {
		t.Errorf("queued content = %q, want %q", queued.Content, "second")
	}

	close(client.release)

	// The first turn completes reporting one message still waiting, that message is
	// announced as starting, and its own turn completes with nothing left.
	var sawCompletedWithRemaining, sawDequeued, sawFinalCompleted bool
	for i := 0; i < 12 && !sawFinalCompleted; i++ {
		env = readEnvelope(t, conn)
		switch env.Type {
		case wsconn.TypeMessageDequeued:
			sawDequeued = true
		case wsconn.TypeMessageCompleted:
			var done wsconn.MessageCompleted
			if err := json.Unmarshal(env.Payload, &done); err != nil {
				t.Fatal(err)
			}
			if done.QueuedRemaining == 1 {
				sawCompletedWithRemaining = true
			}
			if sawDequeued && done.QueuedRemaining == 0 {
				sawFinalCompleted = true
			}
		}
	}
	if !sawCompletedWithRemaining {
		t.Error("the finished turn should report how many messages are still queued")
	}
	if !sawDequeued {
		t.Error("a queued message starting its turn should be announced")
	}
	if !sawFinalCompleted {
		t.Error("the queued message's own turn did not complete")
	}
}
