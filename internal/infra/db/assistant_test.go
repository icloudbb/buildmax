package db

import (
	"errors"
	"strings"
	"testing"

	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	infrasecret "github.com/icloudbb/buildmax/internal/infra/secret"
)

func testAssistantDef(name string) coreassistant.Definition {
	return coreassistant.Definition{
		Name: name, Audience: coreassistant.AudienceSpaceMembers,
		Roster: []coreassistant.RosterEntry{}, ReadableFiles: []string{},
	}
}

// An Assistant starts paused at revision 1; a changed definition appends a
// revision and an unchanged one does not; state and sponsor are not revisions;
// a deleted one is gone from reads and takes its bot with it.
func TestAssistantLifecycle(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "asstowner")
	other := newTestUser(t, s, "asstother")
	space := newTestSpace(t, s, owner)

	a, err := s.CreateAssistant(ctx, space, owner, owner, testAssistantDef("HR"))
	if err != nil {
		t.Fatalf("CreateAssistant: %v", err)
	}
	if a.State != coreassistant.StatePaused || a.Revision != 1 || a.SpaceID != space || a.SponsorUserID != owner || a.Def.Name != "HR" {
		t.Fatalf("created = %+v", a)
	}

	same, err := s.UpdateAssistantDefinition(ctx, a.ID, owner, testAssistantDef("HR"))
	if err != nil || same.Revision != 1 {
		t.Fatalf("unchanged save = %+v, %v; want revision 1", same, err)
	}
	def := testAssistantDef("HR")
	def.Instructions = "Be brief."
	updated, err := s.UpdateAssistantDefinition(ctx, a.ID, other, def)
	if err != nil || updated.Revision != 2 || updated.Def.Instructions != "Be brief." {
		t.Fatalf("changed save = %+v, %v", updated, err)
	}
	var revisions int64
	s.db.Model(&assistantRevisionRow{}).Joins("JOIN assistant a ON a.id = assistant_revision.assistant_id").
		Where("a.public_id = ?", a.ID).Count(&revisions)
	if revisions != 2 {
		t.Errorf("revision rows = %d, want 2", revisions)
	}

	if err := s.SetAssistantState(ctx, a.ID, coreassistant.StateActive); err != nil {
		t.Fatal(err)
	}
	if err := s.SetAssistantSponsor(ctx, a.ID, other); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetAssistant(ctx, a.ID)
	if got == nil || got.State != coreassistant.StateActive || got.SponsorUserID != other || got.Revision != 2 {
		t.Fatalf("after state and sponsor = %+v", got)
	}
	list, err := s.ListAssistantsBySpace(ctx, space)
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("ListAssistantsBySpace = %+v, %v", list, err)
	}

	if err := s.DeleteAssistant(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetAssistant(ctx, a.ID); got != nil {
		t.Error("a deleted assistant is still read")
	}
	if err := s.SetAssistantState(ctx, a.ID, coreassistant.StatePaused); !errors.Is(err, coreassistant.ErrNotFound) {
		t.Errorf("state on a deleted assistant = %v", err)
	}
	if err := s.DeleteAssistant(ctx, a.ID); !errors.Is(err, coreassistant.ErrNotFound) {
		t.Errorf("second delete = %v", err)
	}
}

// A bot token is stored sealed and read back only through BindingToken; one
// binding per Assistant and one per bot; a deployment without a key refuses.
func TestAssistantBindingSealsTheToken(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "bindowner")
	space := newTestSpace(t, s, owner)
	rot := newKEKRotation(t)
	a, err := s.CreateAssistant(ctx, space, owner, owner, testAssistantDef("Bot"))
	if err != nil {
		t.Fatal(err)
	}
	b2, err := s.CreateAssistant(ctx, space, owner, owner, testAssistantDef("Bot two"))
	if err != nil {
		t.Fatal(err)
	}
	botID := "bot-" + testPublicID(t)
	t.Cleanup(func() { _ = s.db.Delete(&assistantBindingRow{}, "bot_id LIKE ?", botID+"%").Error })

	in := coreassistant.NewBinding{AssistantID: a.ID, Platform: corechannel.PlatformTelegram, BotID: botID, BotHandle: "@hr_bot", Token: "123:SECRET", CreatedBy: owner}
	s.SetCredentialCipher(nil)
	if _, err := s.CreateBinding(ctx, in); !errors.Is(err, coregw.ErrCredentialEncryptionUnavailable) {
		t.Fatalf("binding without a key = %v", err)
	}
	s.SetCredentialCipher(rot.before)
	b, err := s.CreateBinding(ctx, in)
	if err != nil {
		t.Fatalf("CreateBinding: %v", err)
	}
	if b.AssistantID != a.ID || b.SpaceID != space || b.BotHandle != "@hr_bot" {
		t.Errorf("binding = %+v", b)
	}
	var row assistantBindingRow
	if err := s.db.Where("public_id = ?", b.ID).Take(&row).Error; err != nil || strings.Contains(string(row.TokenSealed), "SECRET") {
		t.Fatalf("stored token = %q, %v; want it sealed", row.TokenSealed, err)
	}
	if token, err := s.BindingToken(ctx, b.ID); err != nil || token != "123:SECRET" {
		t.Fatalf("BindingToken = %q, %v", token, err)
	}

	if _, err := s.CreateBinding(ctx, coreassistant.NewBinding{AssistantID: a.ID, Platform: corechannel.PlatformTelegram, BotID: botID + "x", Token: "t", CreatedBy: owner}); !errors.Is(err, coreassistant.ErrAlreadyBound) {
		t.Errorf("second binding for one assistant = %v", err)
	}
	if _, err := s.CreateBinding(ctx, coreassistant.NewBinding{AssistantID: b2.ID, Platform: corechannel.PlatformTelegram, BotID: botID, Token: "t", CreatedBy: owner}); !errors.Is(err, corechannel.ErrBotInUse) {
		t.Errorf("one bot for two assistants = %v", err)
	}

	// The KEK rotation moves bot tokens with everything else.
	res, err := s.RewrapSealedKeys(ctx, rot.during)
	if err != nil || res.Rewrapped[rot.oldID] < 1 {
		t.Fatalf("rewrap = %+v, %v", res, err)
	}
	s.SetCredentialCipher(infrasecret.NewCipher(rot.after))
	if token, err := s.BindingToken(ctx, b.ID); err != nil || token != "123:SECRET" {
		t.Fatalf("token after rewrap = %q, %v", token, err)
	}

	if got, err := s.ListBindings(ctx); err != nil || !containsBinding(got, b.ID) {
		t.Errorf("ListBindings lacks the binding: %v", err)
	}
	if err := s.DeleteAssistant(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetBinding(ctx, b.ID); got != nil {
		t.Error("deleting the assistant left its binding")
	}
	if err := s.DeleteBindingByAssistant(ctx, b2.ID); !errors.Is(err, coreassistant.ErrNotBound) {
		t.Errorf("unbinding an unbound assistant = %v", err)
	}
}

func containsBinding(list []coreassistant.Binding, id string) bool {
	for _, b := range list {
		if b.ID == id {
			return true
		}
	}
	return false
}

// An Assistant's conversation is found by Assistant, requester, and chat, not
// by bot, and a Task or Workflow run it starts reads back its provenance.
func TestAssistantConversationAndProvenance(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "convowner")
	requester := newTestUser(t, s, "convrequester")
	space := newTestSpace(t, s, owner)
	a, err := s.CreateAssistant(ctx, space, owner, owner, testAssistantDef("Front"))
	if err != nil {
		t.Fatal(err)
	}

	if got, err := s.LatestAssistantConversation(ctx, a.ID, requester, "telegram", "chat-1"); err != nil || got != nil {
		t.Fatalf("before any = %+v, %v", got, err)
	}
	first, err := s.CreateAssistantConversation(ctx, a.ID, space, requester, "telegram", "bind_old", "chat-1")
	if err != nil {
		t.Fatalf("CreateAssistantConversation: %v", err)
	}
	// A rebound bot has a new connector key; the conversation still continues.
	got, err := s.LatestAssistantConversation(ctx, a.ID, requester, "telegram", "chat-1")
	if err != nil || got == nil || got.ID != first.ID || got.AssistantID != a.ID || got.UserID != requester || got.SpaceID != space {
		t.Fatalf("latest = %+v, %v", got, err)
	}
	if other, _ := s.LatestAssistantConversation(ctx, a.ID, owner, "telegram", "chat-1"); other != nil {
		t.Error("another requester's conversation was found")
	}
	if personal, _ := s.LatestChatConversation(ctx, requester, "telegram", "bind_old", "chat-1"); personal != nil {
		t.Errorf("chat lookup returned the assistant's conversation: %+v", personal)
	}

	msg, err := s.AppendMessage(ctx, coreconv.AppendInput{ConversationID: first.ID, Role: "user", Content: "hi", AssistantRevision: 4})
	if err != nil {
		t.Fatal(err)
	}
	if stored, _ := s.ListMessages(ctx, first.ID); len(stored) != 1 || stored[0].ID != msg.ID || stored[0].AssistantRevision != 4 {
		t.Errorf("messages = %+v", stored)
	}

	tk, err := s.CreateTask(ctx, &coretask.CreateInput{
		ConversationID: first.ID, SpaceID: space, Input: "look up leave", CreatedBy: owner,
		RequestedBy: requester, AssistantID: a.ID, AssistantRevision: 4,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	read, err := s.GetTask(ctx, tk.ID)
	if err != nil || read.RequestedBy != requester || read.AssistantID != a.ID || read.AssistantRevision != 4 || read.CreatedBy != owner {
		t.Errorf("task = %+v, %v", read, err)
	}
	if tk.RequestedBy != requester || tk.AssistantID != a.ID {
		t.Errorf("created task = %+v", tk)
	}

	wf, err := s.CreateWorkflow(ctx, space, owner, "wf", "", `{"schema_version":1,"nodes":[]}`)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.CreateWorkflowRun(ctx, coreworkflow.CreateRunInput{
		WorkflowID: wf.ID, ConversationID: &first.ID, Status: string(coreworkflow.RunStatusRunning), CreatedBy: owner,
	})
	if err != nil {
		t.Fatalf("CreateWorkflowRun: %v", err)
	}
	if got, err := s.GetWorkflowRun(ctx, run.ID); err != nil || got.ConversationID == nil || *got.ConversationID != first.ID {
		t.Errorf("workflow run = %+v, %v", got, err)
	}
}
