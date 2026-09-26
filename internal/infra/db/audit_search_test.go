package db

import (
	"context"
	"os"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
)

// TestSearchAuditEvents covers the read the space-scoped method cannot do: an
// event with no space. A login, a grant, and an account action are all in that
// shape, so a search that could not reach them would leave the trail's most
// sensitive half unreadable.
func TestSearchAuditEvents(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	actor := newTestUser(t, s, "audit")
	spaceID := newTestSpace(t, s, actor)
	t.Cleanup(func() { _ = s.db.WithContext(ctx).Delete(&auditEventRow{}, "actor_id = ?", actor).Error })

	for _, e := range []coreaudit.Event{
		{ActorType: coreaudit.ActorUser, ActorID: actor, Action: coreaudit.UserLogin},
		{ActorType: coreaudit.ActorSystem, ActorID: actor, Action: coreaudit.SystemAdminGranted, TargetType: "user", TargetID: actor},
		{SpaceID: spaceID, ActorType: coreaudit.ActorUser, ActorID: actor, Action: coreaudit.SpaceMemberAdded},
	} {
		if err := s.RecordAuditEvent(ctx, e); err != nil {
			t.Fatalf("RecordAuditEvent: %v", err)
		}
	}

	all, total, err := s.SearchAuditEvents(ctx, coreaudit.Filter{ActorID: actor}, 50, 0)
	if err != nil || total != 3 || len(all) != 3 {
		t.Fatalf("search by actor = %d of %d, %v", len(all), total, err)
	}
	// Newest first, and the ordering is stable rather than whatever the engine
	// returns: an investigation reads down a page.
	for i := 1; i < len(all); i++ {
		if all[i-1].CreatedAt.Before(all[i].CreatedAt) {
			t.Errorf("results are not newest first: %+v", all)
			break
		}
	}

	spaceOnly, total, err := s.SearchAuditEvents(ctx, coreaudit.Filter{ActorID: actor, SpaceID: spaceID}, 50, 0)
	if err != nil || total != 1 || spaceOnly[0].Action != coreaudit.SpaceMemberAdded {
		t.Errorf("search by space = %+v, %d, %v", spaceOnly, total, err)
	}

	// The events a space-scoped reader can never see, asked for on purpose.
	noSpace, total, err := s.SearchAuditEvents(ctx, coreaudit.Filter{ActorID: actor, WithoutSpace: true}, 50, 0)
	if err != nil || total != 2 {
		t.Fatalf("deployment-scoped search = %d, %v", total, err)
	}
	for _, e := range noSpace {
		if e.SpaceID != "" {
			t.Errorf("WithoutSpace returned a space-scoped event: %+v", e)
		}
	}

	byAction, total, err := s.SearchAuditEvents(ctx, coreaudit.Filter{ActorID: actor, Action: coreaudit.SystemAdminGranted}, 50, 0)
	if err != nil || total != 1 || byAction[0].TargetID != actor {
		t.Errorf("search by action = %+v, %d, %v", byAction, total, err)
	}

	// The same actor, asked through the space-scoped method, sees only the one
	// event that has a space.
	spaceScoped, total, err := s.ListAuditEvents(ctx, spaceID, 50, 0)
	if err != nil || total != 1 || len(spaceScoped) != 1 {
		t.Errorf("ListAuditEvents = %d of %d, %v", len(spaceScoped), total, err)
	}
}

// An event recorded while serving a run inherits that run's id from the context,
// so a governance action a run caused is reachable from the run without every
// call site threading the id — and an event that names a run explicitly keeps
// its own id rather than the ambient one.
func TestRecordAuditEventStampsRunFromContext(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	actor := newTestUser(t, s, "auditrun")
	t.Cleanup(func() { _ = s.db.WithContext(ctx).Delete(&auditEventRow{}, "actor_id = ?", actor).Error })

	runCtx := coreaudit.ContextWithRun(ctx, "r_ctx")
	// Inherits the run from the context.
	if err := s.RecordAuditEvent(runCtx, coreaudit.Event{ActorType: coreaudit.ActorWorker, ActorID: actor, Action: coreaudit.ArtifactCreated}); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}
	// Names its own run, which wins over the context's.
	if err := s.RecordAuditEvent(runCtx, coreaudit.Event{ActorType: coreaudit.ActorWorker, ActorID: actor, Action: coreaudit.ArtifactDeleted, TaskRunID: "r_explicit"}); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}
	// No run in context and none named: the id stays empty.
	if err := s.RecordAuditEvent(ctx, coreaudit.Event{ActorType: coreaudit.ActorUser, ActorID: actor, Action: coreaudit.UserLogin}); err != nil {
		t.Fatalf("RecordAuditEvent: %v", err)
	}

	byRun, total, err := s.SearchAuditEvents(ctx, coreaudit.Filter{ActorID: actor, TaskRunID: "r_ctx"}, 50, 0)
	if err != nil || total != 1 || byRun[0].Action != coreaudit.ArtifactCreated {
		t.Fatalf("filter by inherited run = %+v, %d, %v", byRun, total, err)
	}
	explicit, total, err := s.SearchAuditEvents(ctx, coreaudit.Filter{ActorID: actor, TaskRunID: "r_explicit"}, 50, 0)
	if err != nil || total != 1 || explicit[0].Action != coreaudit.ArtifactDeleted {
		t.Fatalf("filter by explicit run = %+v, %d, %v", explicit, total, err)
	}
	login, _, err := s.SearchAuditEvents(ctx, coreaudit.Filter{ActorID: actor, Action: coreaudit.UserLogin}, 50, 0)
	if err != nil || len(login) != 1 || login[0].TaskRunID != "" {
		t.Fatalf("login should carry no run: %+v, %v", login, err)
	}
}
