package runrelay

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	gws "github.com/gorilla/websocket"
)

func TestAgentWSURL(t *testing.T) {
	got, err := agentWSURL("https://buildmax.example:5678", "tok123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "wss://buildmax.example:5678/api/remote-control/agent-ws?") {
		t.Errorf("scheme/path wrong: %s", got)
	}
	if !strings.Contains(got, "token=tok123") {
		t.Errorf("token missing: %s", got)
	}
	// http downgrades to ws.
	got, _ = agentWSURL("http://localhost:5678/", "t")
	if !strings.HasPrefix(got, "ws://localhost:5678/api/remote-control/agent-ws?") {
		t.Errorf("http did not map to ws: %s", got)
	}
}

// The relay dials out, registers with its identity, receives its session id, and
// relays a content delta.
func TestRelayRegistersAndRelays(t *testing.T) {
	type recv struct {
		typ     string
		payload map[string]any
	}
	got := make(chan recv, 8)
	upgrader := gws.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			var env envelope
			if json.Unmarshal(data, &env) != nil {
				continue
			}
			var p map[string]any
			_ = json.Unmarshal(env.Payload, &p)
			got <- recv{typ: env.Type, payload: p}
			if env.Type == typeAgentRegister {
				reg, _ := json.Marshal(registeredPayload{SessionID: "rcsession0000000000a"})
				out, _ := json.Marshal(envelope{Type: typeAgentRegistered, Payload: reg})
				_ = c.WriteMessage(gws.TextMessage, out)
			}
		}
	}))
	defer server.Close()

	registered := make(chan string, 1)
	r := New(Config{
		ServerURL:    server.URL,
		TokenFunc:    func() (string, error) { return "tok", nil },
		DisplayName:  "myhost — proj",
		Platform:     "cli",
		Host:         "myhost",
		OnRegistered: func(id string) { registered <- id },
	})
	if r == nil {
		t.Fatal("New returned nil for a valid config")
	}
	r.Start(t.Context())
	defer r.Close()

	// First message is the registration carrying our identity.
	select {
	case m := <-got:
		if m.typ != typeAgentRegister {
			t.Fatalf("first message = %q, want register", m.typ)
		}
		if m.payload["platform"] != "cli" || m.payload["host"] != "myhost" {
			t.Errorf("register identity wrong: %+v", m.payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no register received")
	}

	select {
	case id := <-registered:
		if id != "rcsession0000000000a" {
			t.Errorf("session id = %q", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnRegistered never fired")
	}

	// A relayed delta reaches the server.
	r.OnDelta("hello from the laptop")
	select {
	case m := <-got:
		if m.typ != typeAgentEvent {
			t.Fatalf("relayed message = %q, want event", m.typ)
		}
		if m.payload["delta"] != "hello from the laptop" {
			t.Errorf("relayed delta = %v", m.payload["delta"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("delta never relayed")
	}
}

// An inbound prompt the server sends reaches OnRemotePrompt.
func TestRelayReceivesRemotePrompt(t *testing.T) {
	upgrader := gws.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		// Wait for the register, then push a prompt.
		if _, _, err := c.ReadMessage(); err != nil {
			return
		}
		reg, _ := json.Marshal(registeredPayload{SessionID: "s1"})
		_ = c.WriteMessage(gws.TextMessage, mustEnvelope(typeAgentRegistered, reg))
		pr, _ := json.Marshal(promptPayload{Content: "run the tests"})
		_ = c.WriteMessage(gws.TextMessage, mustEnvelope(typeAgentPrompt, pr))
		// Keep the socket open so the client reads both frames.
		_, _, _ = c.ReadMessage()
	}))
	defer server.Close()

	prompts := make(chan string, 1)
	r := New(Config{
		ServerURL:      server.URL,
		TokenFunc:      func() (string, error) { return "tok", nil },
		OnRemotePrompt: func(content string) { prompts <- content },
	})
	r.Start(t.Context())
	defer r.Close()

	select {
	case got := <-prompts:
		if got != "run the tests" {
			t.Errorf("remote prompt = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OnRemotePrompt never fired")
	}
}

func mustEnvelope(typ string, payload json.RawMessage) []byte {
	out, _ := json.Marshal(envelope{Type: typ, Payload: payload})
	return out
}

// New returns nil when there is nothing to connect to, and the nil relay's
// methods are safe no-ops.
func TestNewInertWithoutServer(t *testing.T) {
	if r := New(Config{TokenFunc: func() (string, error) { return "t", nil }}); r != nil {
		t.Error("expected nil relay without a server URL")
	}
	var r *Relay
	r.Start(t.Context())
	r.OnDelta("x")
	_ = r.Close()
}
