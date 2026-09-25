package agentapp

import (
	"context"
	"errors"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/core/session"
)

// A local session keeps the mode of its first turn. Signing in afterwards must
// not replay its history to the deployment, and signing out must not replay a
// deployment's conversation to a personal key.
func TestSessionIsBoundToTheModeOfItsFirstTurn(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSessionManager(dir).ForProject("proj_1")
	local := &AgentApp{sessionManager: mgr, llmClients: &LLMClientCache{}}
	managed := &AgentApp{sessionManager: mgr, llmClients: &LLMClientCache{managedServerURL: "https://buildmax.example.com"}}
	ctx := context.Background()

	sess, err := mgr.Create("m")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := local.bindPromptDestination(ctx, sess); err != nil {
		t.Fatalf("first local turn: %v", err)
	}
	if err := local.bindPromptDestination(ctx, sess); err != nil {
		t.Errorf("a second local turn was refused: %v", err)
	}
	if err := managed.bindPromptDestination(ctx, sess); !errors.Is(err, session.ErrDestinationMismatch) {
		t.Errorf("a signed-in turn on a local session = %v, want ErrDestinationMismatch", err)
	}
	id := sess.ID()
	_ = sess.Close()

	// The binding is durable: it survives closing and reopening the session.
	reopened, err := mgr.Open(id, "m")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.Meta().PromptDestination; got != session.DestinationLocal {
		t.Errorf("persisted destination = %q, want %q", got, session.DestinationLocal)
	}
	if err := managed.bindPromptDestination(ctx, reopened); !errors.Is(err, session.ErrDestinationMismatch) {
		t.Errorf("a reopened local session accepted a signed-in turn: %v", err)
	}
}

// A task run's session has no local Project; it belongs to the deployment
// that owns the run, so it is not bound here.
func TestTaskRunSessionsAreNotBound(t *testing.T) {
	mgr := NewSessionManager(t.TempDir())
	app := &AgentApp{sessionManager: mgr, llmClients: &LLMClientCache{managedServerURL: "http://server:5678"}}
	sess, err := mgr.Create("m")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	if err := app.bindPromptDestination(context.Background(), sess); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if sess.Meta().PromptDestination != "" {
		t.Errorf("a task-run session was bound to %q", sess.Meta().PromptDestination)
	}
}

// A fork copies its parent's history, so it inherits the parent's binding;
// forking must not be a way to replay that history in the other mode.
func TestAForkKeepsItsParentsDestination(t *testing.T) {
	mgr := NewSessionManager(t.TempDir()).ForProject("proj_1")
	managed := &AgentApp{sessionManager: mgr, llmClients: &LLMClientCache{managedServerURL: "https://buildmax.example.com"}}
	local := &AgentApp{sessionManager: mgr, llmClients: &LLMClientCache{}}
	ctx := context.Background()

	parent, err := mgr.Create("m")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { _ = parent.Close() })
	if err := managed.bindPromptDestination(ctx, parent); err != nil {
		t.Fatalf("bind parent: %v", err)
	}
	if err := parent.Append(llm.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	ids := parent.MessageIDs()
	child, err := mgr.Fork(parent, ids[len(ids)-1], "m")
	if err != nil {
		t.Fatalf("Fork: %v", err)
	}
	t.Cleanup(func() { _ = child.Close() })
	if err := local.bindPromptDestination(ctx, child); !errors.Is(err, session.ErrDestinationMismatch) {
		t.Errorf("a fork of a managed session ran in local mode: %v", err)
	}
}
