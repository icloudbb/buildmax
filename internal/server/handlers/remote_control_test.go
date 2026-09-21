package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coreremote "github.com/icloudbb/buildmax/internal/core/remotesession"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
	"github.com/icloudbb/buildmax/internal/testsupport"

	gws "github.com/gorilla/websocket"
)

// fakeRemoteStore records the calls the agent socket and the HTTP routes make.
type fakeRemoteStore struct {
	mu         sync.Mutex
	id         string
	registered coreremote.NewRemoteSession
	touched    int
	offline    bool
	list       []coreremote.RemoteSession
	get        map[string]coreremote.RemoteSession
}

func (f *fakeRemoteStore) RegisterRemoteSession(_ context.Context, in coreremote.NewRemoteSession) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = in
	if f.id == "" {
		f.id = "rcsession0000000000a"
	}
	return f.id, nil
}

func (f *fakeRemoteStore) TouchRemoteSession(_ context.Context, _ string, _ time.Time) error {
	f.mu.Lock()
	f.touched++
	f.mu.Unlock()
	return nil
}

func (f *fakeRemoteStore) MarkRemoteSessionOffline(_ context.Context, _ string, _ time.Time) error {
	f.mu.Lock()
	f.offline = true
	f.mu.Unlock()
	return nil
}

func (f *fakeRemoteStore) MarkStaleRemoteSessionsOffline(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

func (f *fakeRemoteStore) GetRemoteSession(_ context.Context, id string) (coreremote.RemoteSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.get[id]; ok {
		return s, nil
	}
	return coreremote.RemoteSession{}, coreremote.ErrNotFound
}

func (f *fakeRemoteStore) setGet(s coreremote.RemoteSession) {
	f.mu.Lock()
	if f.get == nil {
		f.get = map[string]coreremote.RemoteSession{}
	}
	f.get[s.ID] = s
	f.mu.Unlock()
}

func (f *fakeRemoteStore) ListRemoteSessionsByUser(_ context.Context, _ string) ([]coreremote.RemoteSession, error) {
	return f.list, nil
}

func (f *fakeRemoteStore) counters() (touched int, offline bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.touched, f.offline
}

func dialAgentWS(t *testing.T, server *httptest.Server, token string) *gws.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/remote-control/agent-ws?token=" + token
	conn, resp, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial agent ws: %v (resp=%v)", err, resp)
	}
	return conn
}

// A local session registers, its heartbeat and events reach the server, and a
// disconnect marks it offline and closes the stream.
func TestAgentWSRegisterRelayAndDisconnect(t *testing.T) {
	store := &fakeRemoteStore{}
	h := NewHandler(Config{JWTSecret: wsTestSecret, CORSOrigin: "*", RemoteSessionStore: store})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	conn := dialAgentWS(t, server, testsupport.SignJWT("u1", wsTestSecret))

	sendEnvelope(t, conn, wsconn.TypeAgentRegister, wsconn.AgentRegister{
		DisplayName: "myhost — proj", Platform: "cli", Host: "myhost",
	})
	env := readEnvelope(t, conn)
	if env.Type != wsconn.TypeAgentRegistered {
		t.Fatalf("first event = %q, want %q", env.Type, wsconn.TypeAgentRegistered)
	}
	var reg wsconn.AgentRegistered
	if err := json.Unmarshal(env.Payload, &reg); err != nil {
		t.Fatal(err)
	}
	if reg.SessionID != store.id {
		t.Fatalf("registered session id = %q, want %q", reg.SessionID, store.id)
	}
	if store.registered.Platform != "cli" || store.registered.Host != "myhost" {
		t.Errorf("register did not carry platform/host: %+v", store.registered)
	}

	// A relayed event reaches the session's stream.
	events, unsub := h.hub.Subscribe(reg.SessionID)
	defer unsub()
	sendEnvelope(t, conn, wsconn.TypeAgentEvent, wsconn.AgentEvent{Delta: "hello from the laptop"})
	select {
	case msg := <-events:
		if msg != "hello from the laptop" {
			t.Errorf("relayed delta = %q", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relayed event never reached the stream")
	}

	// A heartbeat touches presence.
	sendEnvelope(t, conn, wsconn.TypeAgentHeartbeat, struct{}{})
	waitFor(t, func() bool { n, _ := store.counters(); return n > 0 }, "heartbeat was not recorded")

	// Closing the socket marks the session offline and ends the stream.
	_ = conn.Close()
	waitFor(t, func() bool { _, off := store.counters(); return off }, "disconnect did not mark the session offline")
	select {
	case msg := <-events:
		if msg != wsconn.StreamEventDone {
			t.Errorf("stream end = %q, want done", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream was not closed on disconnect")
	}
}

func TestAgentWSRequiresToken(t *testing.T) {
	h := NewHandler(Config{JWTSecret: wsTestSecret, CORSOrigin: "*", RemoteSessionStore: &fakeRemoteStore{}})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/remote-control/agent-ws"
	_, resp, err := gws.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		t.Fatal("expected error for missing token")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
}

// A follow-up prompt POSTed by the owner reaches the session's agent socket over
// the WebSocket, as agent.prompt.
func TestPromptDeliveredToAgentSocket(t *testing.T) {
	store := &fakeRemoteStore{}
	h := NewHandler(Config{JWTSecret: wsTestSecret, CORSOrigin: "*", RemoteSessionStore: store})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	conn := dialAgentWS(t, server, testsupport.SignJWT("u1", wsTestSecret))
	defer conn.Close()
	sendEnvelope(t, conn, wsconn.TypeAgentRegister, wsconn.AgentRegister{Platform: "cli"})
	reg := readEnvelope(t, conn)
	if reg.Type != wsconn.TypeAgentRegistered {
		t.Fatalf("first event = %q, want registered", reg.Type)
	}
	var registered wsconn.AgentRegistered
	if err := json.Unmarshal(reg.Payload, &registered); err != nil {
		t.Fatal(err)
	}

	// The session must resolve as online and owned by u1 for the POST to pass.
	store.setGet(coreremote.RemoteSession{ID: registered.SessionID, UserID: "u1", Status: coreremote.StatusOnline})

	req, _ := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/remote-control/sessions/"+registered.SessionID+"/prompt",
		strings.NewReader(`{"content":"do the thing"}`),
	)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", wsTestSecret))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	env := readEnvelope(t, conn)
	if env.Type != wsconn.TypeAgentPrompt {
		t.Fatalf("agent received %q, want prompt", env.Type)
	}
	var prompt wsconn.AgentPrompt
	if err := json.Unmarshal(env.Payload, &prompt); err != nil {
		t.Fatal(err)
	}
	if prompt.Content != "do the thing" {
		t.Errorf("prompt content = %q", prompt.Content)
	}
}

// A prompt for an offline session is refused with 409, not delivered.
func TestPromptRejectedWhenOffline(t *testing.T) {
	store := &fakeRemoteStore{}
	store.setGet(coreremote.RemoteSession{ID: "s9", UserID: "u1", Status: coreremote.StatusOffline})
	h := NewHandler(Config{JWTSecret: wsTestSecret, RemoteSessionStore: store})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/remote-control/sessions/s9/prompt",
		strings.NewReader(`{"content":"hi"}`))
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", wsTestSecret))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Errorf("status = %d, want 409", resp.StatusCode)
	}
}

func TestListRemoteSessions(t *testing.T) {
	store := &fakeRemoteStore{list: []coreremote.RemoteSession{
		{ID: "s1", UserID: "u1", DisplayName: "one", Platform: "cli", Status: coreremote.StatusOnline, CreatedAt: time.Now()},
	}}
	h := NewHandler(Config{JWTSecret: wsTestSecret, RemoteSessionStore: store})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	body := getJSON(t, server, "/api/remote-control/sessions", testsupport.SignJWT("u1", wsTestSecret))
	if !strings.Contains(body, `"id":"s1"`) || !strings.Contains(body, `"status":"online"`) {
		t.Errorf("list response missing session: %s", body)
	}
}

// A session owned by someone else is reported as not found, so ownership is not
// probeable.
func TestRemoteSessionStreamRejectsForeignOwner(t *testing.T) {
	store := &fakeRemoteStore{get: map[string]coreremote.RemoteSession{
		"s2": {ID: "s2", UserID: "someone-else"},
	}}
	h := NewHandler(Config{JWTSecret: wsTestSecret, RemoteSessionStore: store})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/remote-control/sessions/s2/stream", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", wsTestSecret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func getJSON(t *testing.T, server *httptest.Server, path, token string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, server.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	b, _ := io.ReadAll(resp.Body)
	return string(b)
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}
