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
	mu            sync.Mutex
	id            string
	registered    coreremote.NewRemoteSession
	registerCount int
	touched       int
	offline       bool
	list          []coreremote.RemoteSession
	get           map[string]coreremote.RemoteSession
}

func (f *fakeRemoteStore) RegisterRemoteSession(_ context.Context, in coreremote.NewRemoteSession) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = in
	f.registerCount++
	if f.id == "" {
		f.id = "rcsession0000000000a"
	}
	return f.id, nil
}

func (f *fakeRemoteStore) registers() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.registerCount
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

	// Closing the socket marks the session offline, but does NOT end the stream:
	// the session may reconnect and reattach, so a watching device keeps its
	// stream open across the gap.
	_ = conn.Close()
	waitFor(t, func() bool { _, off := store.counters(); return off }, "disconnect did not mark the session offline")
	select {
	case msg := <-events:
		t.Errorf("stream should stay open across a disconnect, got %q", msg)
	case <-time.After(200 * time.Millisecond):
		// Expected: the stream stays open.
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

// An approval decision POSTed by the owner reaches the agent socket as
// agent.approval_response.
func TestApprovalDeliveredToAgentSocket(t *testing.T) {
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
	var registered wsconn.AgentRegistered
	if err := json.Unmarshal(reg.Payload, &registered); err != nil {
		t.Fatal(err)
	}
	store.setGet(coreremote.RemoteSession{ID: registered.SessionID, UserID: "u1", Status: coreremote.StatusOnline})

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/remote-control/sessions/"+registered.SessionID+"/approval",
		strings.NewReader(`{"id":"a1","decision":"session"}`))
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
	if env.Type != wsconn.TypeAgentApprovalResponse {
		t.Fatalf("agent received %q, want approval_response", env.Type)
	}
	var ar wsconn.AgentApprovalResponse
	if err := json.Unmarshal(env.Payload, &ar); err != nil {
		t.Fatal(err)
	}
	if ar.ID != "a1" || ar.Decision != "session" {
		t.Errorf("approval response = %+v", ar)
	}
}

// An approval request the agent raises reaches the session's approval stream.
func TestAgentApprovalReachesStream(t *testing.T) {
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
	var registered wsconn.AgentRegistered
	if err := json.Unmarshal(reg.Payload, &registered); err != nil {
		t.Fatal(err)
	}

	events, unsub := h.hub.Subscribe(wsconn.ApprovalStreamKey(registered.SessionID))
	defer unsub()
	sendEnvelope(t, conn, wsconn.TypeAgentApproval, wsconn.AgentApproval{ID: "a1", Tool: "bash", Summary: `{"cmd":"ls"}`})
	select {
	case frame := <-events:
		if !strings.Contains(frame, `"id":"a1"`) || !strings.Contains(frame, `"tool":"bash"`) {
			t.Errorf("approval frame = %q", frame)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("approval frame never reached the stream")
	}
}

// A cancel POSTed by the owner reaches the agent socket as agent.cancel.
func TestCancelDeliveredToAgentSocket(t *testing.T) {
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
	var registered wsconn.AgentRegistered
	if err := json.Unmarshal(reg.Payload, &registered); err != nil {
		t.Fatal(err)
	}
	store.setGet(coreremote.RemoteSession{ID: registered.SessionID, UserID: "u1", Status: coreremote.StatusOnline})

	req, _ := http.NewRequest(http.MethodPost,
		server.URL+"/api/remote-control/sessions/"+registered.SessionID+"/cancel", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", wsTestSecret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", resp.StatusCode)
	}

	env := readEnvelope(t, conn)
	if env.Type != wsconn.TypeAgentCancel {
		t.Fatalf("agent received %q, want cancel", env.Type)
	}
}

// A reconnect that carries the assigned session id reattaches to the same
// session instead of creating a new one.
func TestReattachKeepsSameSession(t *testing.T) {
	store := &fakeRemoteStore{}
	h := NewHandler(Config{JWTSecret: wsTestSecret, CORSOrigin: "*", RemoteSessionStore: store})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	// First connection: a fresh registration assigns an id.
	conn1 := dialAgentWS(t, server, testsupport.SignJWT("u1", wsTestSecret))
	sendEnvelope(t, conn1, wsconn.TypeAgentRegister, wsconn.AgentRegister{Platform: "cli"})
	var first wsconn.AgentRegistered
	if err := json.Unmarshal(readEnvelope(t, conn1).Payload, &first); err != nil {
		t.Fatal(err)
	}
	store.setGet(coreremote.RemoteSession{ID: first.SessionID, UserID: "u1", Status: coreremote.StatusOnline})
	_ = conn1.Close()

	// Second connection: reattach by id.
	conn2 := dialAgentWS(t, server, testsupport.SignJWT("u1", wsTestSecret))
	defer conn2.Close()
	sendEnvelope(t, conn2, wsconn.TypeAgentRegister, wsconn.AgentRegister{SessionID: first.SessionID, Platform: "cli"})
	var second wsconn.AgentRegistered
	if err := json.Unmarshal(readEnvelope(t, conn2).Payload, &second); err != nil {
		t.Fatal(err)
	}
	if second.SessionID != first.SessionID {
		t.Errorf("reattach id = %q, want %q", second.SessionID, first.SessionID)
	}
	if n := store.registers(); n != 1 {
		t.Errorf("RegisterRemoteSession called %d times, want 1 (reattach must not create a new session)", n)
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

// An idle stream must keep emitting SSE comment frames, so a proxy read timeout
// does not close a quiet session's stream and the watching device is not left
// reading a dead connection. See F1 in the 2026-09-23 exploratory run.
func TestRemoteSessionStreamEmitsHeartbeat(t *testing.T) {
	old := sseHeartbeatInterval
	sseHeartbeatInterval = 15 * time.Millisecond
	defer func() { sseHeartbeatInterval = old }()

	store := &fakeRemoteStore{get: map[string]coreremote.RemoteSession{
		"s1": {ID: "s1", UserID: "u1", Status: coreremote.StatusOnline},
	}}
	h := NewHandler(Config{JWTSecret: wsTestSecret, RemoteSessionStore: store})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/remote-control/sessions/s1/stream", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", wsTestSecret))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// The session relays nothing, so the only bytes on a working stream are the
	// heartbeat comments. Read until one arrives or the deadline fires.
	buf := make([]byte, 0, 256)
	tmp := make([]byte, 64)
	for !strings.Contains(string(buf), ": ping") {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			t.Fatalf("idle stream never emitted a heartbeat; read %q then %v", buf, err)
		}
	}
}
