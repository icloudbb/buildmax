package db

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"

	"github.com/icloudbb/buildmax/internal/util"
)

// testPublicID is a distinct handle for a row a test invents. It is a real
// public ID rather than a readable string so a test cannot pass on a value the
// codec would reject.
func testPublicID(t *testing.T) string {
	t.Helper()
	id, err := util.NewPublicID()
	if err != nil {
		t.Fatalf("NewPublicID: %v", err)
	}
	return id
}

// newTestUser creates a real account and registers its removal.
//
// A login code, a refresh token, a webhook key, and a grant all reference a
// user row now, so a test can no longer invent a handle for one: an unknown
// handle resolves to no row and the write is refused. That refusal is the
// point, which makes creating the account the fixture rather than a detour.
func newTestUser(t *testing.T, s *Store, label string) string {
	t.Helper()
	// Not t.Context(): it is cancelled before cleanups run.
	ctx := context.Background()
	u, err := s.CreateUser(ctx, label+"-"+testPublicID(t)+"@example.com", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t.Cleanup(func() { deleteTestUser(t, s, u.ID) })
	return u.ID
}

// newTestSpace creates a space owned by userID and registers its removal.
func newTestSpace(t *testing.T, s *Store, userID string) string {
	t.Helper()
	ctx := context.Background()
	space, err := s.CreateSpace(ctx, "Test "+testPublicID(t), userID, "")
	if err != nil {
		t.Fatalf("CreateSpace: %v", err)
	}
	t.Cleanup(func() {
		key, err := lookupKey(ctx, s.db, "space", space.ID)
		if err != nil {
			return
		}
		db := s.db.WithContext(ctx)
		_ = db.Delete(&spaceMemberRow{}, "space_id = ?", key).Error
		_ = db.Delete(&spaceRow{}, "id = ?", key).Error
	})
	return space.ID
}

// deleteTestUser removes an account and everything keyed to it. Nothing
// cascades, so the order here is the order the references run.
func deleteTestUser(t *testing.T, s *Store, userID string) {
	t.Helper()
	ctx := context.Background()
	key, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return
	}
	if err != nil {
		t.Errorf("cleanup: resolve %s: %v", userID, err)
		return
	}
	db := s.db.WithContext(ctx)
	for _, del := range []func() error{
		func() error { return db.Delete(&loginCodeRow{}, "user_id = ?", key).Error },
		func() error { return db.Delete(&userRefreshTokenRow{}, "user_id = ?", key).Error },
		func() error { return db.Delete(&authSessionRow{}, "user_id = ?", key).Error },
		func() error { return db.Delete(&userWebhookKeyRow{}, "user_id = ?", key).Error },
		func() error { return db.Delete(&systemGrantRow{}, "user_id = ?", key).Error },
		func() error { return db.Delete(&spaceMemberRow{}, "user_id = ?", key).Error },
		func() error { return db.Delete(&spaceRow{}, "personal_for_user_id = ?", key).Error },
		func() error { return db.Delete(&userRow{}, "id = ?", key).Error },
	} {
		if err := del(); err != nil {
			t.Errorf("cleanup: delete rows for %s: %v", userID, err)
		}
	}
}

func TestCreateUser(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	email := "createuser-test@example.com"
	// Clean up if a previous run left the user.
	if existing, _ := s.UserByEmail(ctx, email); existing != nil {
		deleteTestUser(t, s, existing.ID)
	}
	t.Cleanup(func() {
		if u, _ := s.UserByEmail(context.Background(), email); u != nil {
			deleteTestUser(t, s, u.ID)
		}
	})

	u, err := s.CreateUser(ctx, email, "free_trial")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	defer func() {
		deleteTestUser(t, s, u.ID)
	}()
	if u.Email != email || u.ID == "" {
		t.Errorf("CreateUser: got user %+v", u)
	}

	// UserByEmail should find the new user.
	found, err := s.UserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("UserByEmail: %v", err)
	}
	if found == nil || found.ID != u.ID {
		t.Errorf("UserByEmail: got %+v", found)
	}

	space, err := s.GetPersonalSpaceByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetPersonalSpaceByUser: %v", err)
	}
	if space == nil {
		t.Fatal("GetPersonalSpaceByUser: got nil space")
	}
	if space.Name != corespace.DefaultPersonalName {
		t.Errorf("personal space name = %q, want %q", space.Name, corespace.DefaultPersonalName)
	}
	if space.QuotaTier != "free_trial" {
		t.Errorf("personal space quota_tier = %q, want %q", space.QuotaTier, "free_trial")
	}

	members, err := s.ListSpaceMembers(ctx, space.ID)
	if err != nil {
		t.Fatalf("ListSpaceMembers: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("ListSpaceMembers: got %d members, want 1", len(members))
	}
	if members[0].UserID != u.ID || members[0].Role != corespace.RoleOwner {
		t.Errorf("space member = %+v", members[0])
	}
}

func TestCreateUser_DuplicateEmail(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	email := "dup-test-" + testPublicID(t) + "@example.com"
	u, err := s.CreateUser(ctx, email, "")
	if err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	defer func() {
		deleteTestUser(t, s, u.ID)
	}()

	_, err = s.CreateUser(ctx, email, "")
	if err == nil {
		t.Error("second CreateUser: expected error")
	}
	if !errors.Is(err, coreidentity.ErrEmailExists) {
		t.Errorf("second CreateUser: got %v, want coreidentity.ErrEmailExists", err)
	}
}

func TestCreateSpace(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	user, err := s.CreateUser(ctx, "space-owner-"+testPublicID(t)+"@example.com", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	space, err := s.CreateSpace(ctx, "Ops", user.ID, "free_trial")
	if err != nil {
		t.Fatalf("CreateSpace: %v", err)
	}

	defer func() {
		if key, err := lookupKey(ctx, s.db, "space", space.ID); err == nil {
			_ = s.db.WithContext(ctx).Delete(&spaceMemberRow{}, "space_id = ?", key)
			_ = s.db.WithContext(ctx).Delete(&spaceRow{}, "id = ?", key)
		}
		deleteTestUser(t, s, user.ID)
	}()

	if space.ID == "" || space.Name != "Ops" || space.CreatedBy != user.ID {
		t.Fatalf("created space = %+v", space)
	}
	if space.QuotaTier != "free_trial" {
		t.Fatalf("created space quota_tier = %q, want %q", space.QuotaTier, "free_trial")
	}

	list, err := s.ListSpacesByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("ListSpacesByUser: %v", err)
	}
	if len(list) < 2 {
		t.Fatalf("ListSpacesByUser: got %d spaces, want at least 2", len(list))
	}

	members, err := s.ListSpaceMembers(ctx, space.ID)
	if err != nil {
		t.Fatalf("ListSpaceMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != user.ID || members[0].Role != corespace.RoleOwner {
		t.Fatalf("space members = %+v", members)
	}
}

func TestTaskRunProvenancePersistence(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	provenanceUser := newTestUser(t, s, "provenance")
	conv, err := s.CreateConversation(ctx, provenanceUser, "portal", provenanceUser)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&conversationRow{}, "public_id = ?", canonicalPublicID(conv.ID))
	}()
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:                 conv.SpaceID,
		ConversationID:          conv.ID,
		Input:                   "initial input",
		Title:                   "initial title",
		CreatedBy:               provenanceUser,
		InitialRunCreatedBy:     provenanceUser,
		InitialRunCreatedByType: coretask.RunCreatedByTypeUser,
		InitialRunTriggerSource: coretask.RunTriggerSourcePortalTaskCreate,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.LastRunID == nil {
		t.Fatal("CreateTask should set LastRunID")
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&taskRunRow{}, "task_id = ?", task.ID)
		_ = s.db.WithContext(ctx).Delete(&taskRow{}, "task_id = ?", task.ID)
	}()
	initialRun, err := s.GetTaskRun(ctx, *task.LastRunID)
	if err != nil {
		t.Fatalf("GetTaskRun initial: %v", err)
	}
	if initialRun == nil {
		t.Fatal("initial run not found")
	}
	if initialRun.CreatedBy != provenanceUser {
		t.Fatalf("initial run created_by = %q, want %q", initialRun.CreatedBy, provenanceUser)
	}
	if initialRun.CreatedByType != coretask.RunCreatedByTypeUser {
		t.Fatalf("initial run created_by_type = %q, want %q", initialRun.CreatedByType, coretask.RunCreatedByTypeUser)
	}
	if initialRun.TriggerSource != coretask.RunTriggerSourcePortalTaskCreate {
		t.Fatalf("initial run trigger_source = %q, want %q", initialRun.TriggerSource, coretask.RunTriggerSourcePortalTaskCreate)
	}

	// A task carries at most one run in flight, so a rerun follows a finished
	// run. Asking for one while the first is still PENDING is what the Portal
	// answers 409 to; see CreateTaskRun and coretask.ErrRunInProgress.
	if _, err := s.CreateTaskRun(ctx, coretask.CreateRunInput{
		TaskID:        task.ID,
		Input:         "too soon",
		CreatedBy:     "reviewer-user",
		CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourcePortalConversation,
	}); !errors.Is(err, coretask.ErrRunInProgress) {
		t.Fatalf("CreateTaskRun while the initial run is pending: want ErrRunInProgress, got %v", err)
	}

	endedAt := time.Now().UTC()
	startTaskRunForTest(t, s, ctx, initialRun.ID)
	if updated, err := s.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID:      initialRun.ID,
		ExpectedStatus: coretask.RunStatusRunning,
		NewStatus:      coretask.RunStatusSucceeded,
		EndedAt:        &endedAt,
	}); err != nil || !updated {
		t.Fatalf("TransitionTaskRun to SUCCEEDED: updated=%v err=%v", updated, err)
	}

	rerun, err := s.CreateTaskRun(ctx, coretask.CreateRunInput{
		TaskID:        task.ID,
		Input:         "follow-up input",
		CreatedBy:     "reviewer-user",
		CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourcePortalConversation,
	})
	if err != nil {
		t.Fatalf("CreateTaskRun: %v", err)
	}
	if rerun.CreatedBy != "reviewer-user" {
		t.Fatalf("rerun created_by = %q, want %q", rerun.CreatedBy, "reviewer-user")
	}
	if rerun.CreatedByType != coretask.RunCreatedByTypeUser {
		t.Fatalf("rerun created_by_type = %q, want %q", rerun.CreatedByType, coretask.RunCreatedByTypeUser)
	}
	if rerun.TriggerSource != coretask.RunTriggerSourcePortalConversation {
		t.Fatalf("rerun trigger_source = %q, want %q", rerun.TriggerSource, coretask.RunTriggerSourcePortalConversation)
	}
}

func TestClaimTask(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	user, err := s.CreateUser(ctx, "update-if-user-"+testPublicID(t)+"@example.com", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	conv, err := s.CreateConversation(ctx, user.ID, "portal", user.ID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&conversationRow{}, "public_id = ?", canonicalPublicID(conv.ID))
		deleteTestUser(t, s, user.ID)
	}()
	task, err := s.CreateTask(ctx, &coretask.CreateInput{SpaceID: conv.SpaceID, ConversationID: conv.ID, Input: "input", Title: "", CreatedBy: user.ID})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&taskRunRow{}, "task_id = ?", task.ID)
		_ = s.db.WithContext(ctx).Delete(&taskRow{}, "task_id = ?", task.ID)
	}()
	if task.SpaceID != conv.SpaceID {
		t.Fatalf("task.SpaceID = %q, want %q", task.SpaceID, conv.SpaceID)
	}

	// PENDING -> SCHEDULED: should update
	updated, err := s.ClaimTask(ctx, coretask.ClaimInput{TaskID: task.ID, ExpectedStatus: "PENDING", NewStatus: "SCHEDULED"})
	if err != nil {
		t.Fatalf("ClaimTask PENDING->SCHEDULED: %v", err)
	}
	if !updated {
		t.Error("ClaimTask PENDING->SCHEDULED: want updated true, got false")
	}
	got, _ := s.GetTask(ctx, task.ID)
	if got == nil || got.Status != "SCHEDULED" {
		t.Errorf("after PENDING->SCHEDULED: task status = %q, want SCHEDULED", got.Status)
	}

	// PENDING -> SCHEDULED again: no match, updated false
	updated, err = s.ClaimTask(ctx, coretask.ClaimInput{TaskID: task.ID, ExpectedStatus: "PENDING", NewStatus: "SCHEDULED"})
	if err != nil {
		t.Fatalf("ClaimTask PENDING->SCHEDULED (second): %v", err)
	}
	if updated {
		t.Error("ClaimTask PENDING->SCHEDULED when already SCHEDULED: want updated false, got true")
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got == nil || got.Status != "SCHEDULED" {
		t.Errorf("task status = %q, want SCHEDULED (unchanged)", got.Status)
	}

	// SCHEDULED -> RUNNING: should update
	updated, err = s.ClaimTask(ctx, coretask.ClaimInput{TaskID: task.ID, ExpectedStatus: "SCHEDULED", NewStatus: "RUNNING"})
	if err != nil {
		t.Fatalf("ClaimTask SCHEDULED->RUNNING: %v", err)
	}
	if !updated {
		t.Error("ClaimTask SCHEDULED->RUNNING: want updated true, got false")
	}
	got, _ = s.GetTask(ctx, task.ID)
	if got == nil || got.Status != "RUNNING" {
		t.Errorf("after SCHEDULED->RUNNING: task status = %q, want RUNNING", got.Status)
	}

	// SCHEDULED -> RUNNING again: no match, updated false
	updated, err = s.ClaimTask(ctx, coretask.ClaimInput{TaskID: task.ID, ExpectedStatus: "SCHEDULED", NewStatus: "RUNNING"})
	if err != nil {
		t.Fatalf("ClaimTask SCHEDULED->RUNNING (second): %v", err)
	}
	if updated {
		t.Error("ClaimTask SCHEDULED->RUNNING when already RUNNING: want updated false, got true")
	}
}

func TestIssueStore_CreateListUpdate(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	user, err := s.CreateUser(ctx, "issue-user-"+testPublicID(t)+"@example.com", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	issue, err := s.CreateIssue(ctx, user.ID, coreissue.CreateInput{
		Title:       "Initial issue",
		Description: "Initial description",
	})
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&issueRow{}, "issue_id = ?", issue.ID)
		deleteTestUser(t, s, user.ID)
	}()

	if issue.ID == "" || issue.Status != coreissue.StatusTodo || issue.UserID != user.ID || issue.SpaceID == "" {
		t.Fatalf("created issue = %+v", issue)
	}

	list, total, err := s.ListIssuesByUser(ctx, user.ID, 50, 0)
	if err != nil {
		t.Fatalf("ListIssuesByUser: %v", err)
	}
	if total < 1 || len(list) < 1 {
		t.Fatalf("list total=%d len=%d", total, len(list))
	}

	title := "Updated issue"
	status := coreissue.StatusInProgress
	id := user.ID
	updated, err := s.UpdateIssue(ctx, issue.ID, user.ID, coreissue.UpdateInput{
		IfVersion: issue.Version,
		Title:     &title,
		Status:    &status,
		OwnerID:   &id,
	})
	if err != nil {
		t.Fatalf("UpdateIssue: %v", err)
	}
	if updated == nil || updated.Title != title || updated.Status != status {
		t.Fatalf("updated issue = %+v", updated)
	}
	if updated.OwnerID == nil || *updated.OwnerID != id {
		t.Fatalf("updated owner id = %v, want %v", updated.OwnerID, id)
	}
	if updated.Version != issue.Version+1 {
		t.Fatalf("version = %d, want %d", updated.Version, issue.Version+1)
	}

	// The second writer read the same row the first one did.
	stale := "Written from a stale copy"
	if _, err := s.UpdateIssue(ctx, issue.ID, user.ID, coreissue.UpdateInput{
		IfVersion: issue.Version,
		Title:     &stale,
	}); !errors.Is(err, coreissue.ErrVersionConflict) {
		t.Fatalf("stale UpdateIssue err = %v, want ErrVersionConflict", err)
	}
	current, err := s.GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if current.Title != title {
		t.Fatalf("refused update still wrote: title = %q", current.Title)
	}
}

func TestCreateConversation_AppendMessage_ListMessages(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	user, err := s.CreateUser(ctx, "conv-test-user-"+testPublicID(t)+"@example.com", "")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	conv, err := s.CreateConversation(ctx, user.ID, "portal", user.ID)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Where("conversation_id = ?", conv.ID).Delete(&conversationMessageRow{})
		_ = s.db.WithContext(ctx).Where("public_id = ?", canonicalPublicID(conv.ID)).Delete(&conversationRow{})
		deleteTestUser(t, s, user.ID)
	}()
	if conv.ID == "" || conv.UserID != user.ID || conv.SpaceID == "" || conv.Channel != "portal" {
		t.Errorf("CreateConversation: got %+v", conv)
	}

	ch := "portal"
	m1, err := s.AppendMessage(ctx, coreconv.AppendInput{
		ConversationID: conv.ID, Role: "user", Content: "hello", Channel: &ch,
	})
	if err != nil {
		t.Fatalf("AppendMessage user: %v", err)
	}
	if m1.ID == "" || m1.Role != "user" || m1.Content != "hello" {
		t.Errorf("AppendMessage user: got %+v", m1)
	}

	m2, err := s.AppendMessage(ctx, coreconv.AppendInput{
		ConversationID: conv.ID, Role: "assistant", Content: "hi there",
	})
	if err != nil {
		t.Fatalf("AppendMessage assistant: %v", err)
	}
	if m2.Role != "assistant" || m2.Content != "hi there" {
		t.Errorf("AppendMessage assistant: got %+v", m2)
	}

	tcID := "call_1"
	m3, err := s.AppendMessage(ctx, coreconv.AppendInput{
		ConversationID: conv.ID, Role: "tool", Content: "2025-03-01", ToolCallID: &tcID,
	})
	if err != nil {
		t.Fatalf("AppendMessage tool: %v", err)
	}
	if m3.Role != "tool" || m3.Content != "2025-03-01" || m3.ToolCallID == nil || *m3.ToolCallID != tcID {
		t.Errorf("AppendMessage tool: got %+v", m3)
	}

	sysCh := "system"
	m4, err := s.AppendMessage(ctx, coreconv.AppendInput{
		ConversationID: conv.ID, Role: "system", Content: "[Task Result] internal", Channel: &sysCh,
	})
	if err != nil {
		t.Fatalf("AppendMessage system: %v", err)
	}
	if m4.Role != "system" || m4.Content != "[Task Result] internal" || m4.Channel == nil || *m4.Channel != sysCh {
		t.Errorf("AppendMessage system: got %+v", m4)
	}

	msgs, err := s.ListMessages(ctx, conv.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 4 {
		t.Fatalf("ListMessages: got %d, want 4", len(msgs))
	}
	if msgs[0].Role != "user" || msgs[0].Content != "hello" {
		t.Errorf("ListMessages[0]: got role=%q content=%q", msgs[0].Role, msgs[0].Content)
	}
	if msgs[1].Role != "assistant" || msgs[1].Content != "hi there" {
		t.Errorf("ListMessages[1]: got role=%q content=%q", msgs[1].Role, msgs[1].Content)
	}
	if msgs[2].Role != "tool" || msgs[2].Content != "2025-03-01" {
		t.Errorf("ListMessages[2]: got role=%q content=%q", msgs[2].Role, msgs[2].Content)
	}
	if msgs[3].Role != "system" || msgs[3].Content != "[Task Result] internal" {
		t.Errorf("ListMessages[3]: got role=%q content=%q", msgs[3].Role, msgs[3].Content)
	}
}

func TestListConversationsByUser(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	convListUser := newTestUser(t, s, "conv-list")
	conv, err := s.CreateConversation(ctx, convListUser, "portal", convListUser)
	if err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Where("public_id = ?", canonicalPublicID(conv.ID)).Delete(&conversationRow{})
	}()

	convs, total, err := s.ListConversationsByUser(ctx, convListUser, 10, 0)
	if err != nil {
		t.Fatalf("ListConversationsByUser: %v", err)
	}
	if total < 1 || len(convs) < 1 {
		t.Fatalf("ListConversationsByUser: got %d items, total %d", len(convs), total)
	}
	found := false
	for _, c := range convs {
		if c.ID == conv.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("ListConversationsByUser: did not find created conversation")
	}
}
