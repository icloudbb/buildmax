package db

import (
	"context"
	"os"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
)

// listRevisions and getRevision resolve their table from a type parameter, so
// they compile against any struct and only a real database proves GORM found
// the right one. Agents and workflows both go through them.
func revisionTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, ctx
}

func TestAgentRevisionsPageNewestFirst(t *testing.T) {
	s, ctx := revisionTestStore(t)
	userID := newTestUser(t, s, "revision")
	spaceID := newTestSpace(t, s, userID)

	agent, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{SpaceID: spaceID, UserID: userID,
		Def: agentdef.Definition{Name: "first", Description: "d", Instructions: "i"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	for _, name := range []string{"second", "third"} {
		if _, err := s.UpdateAgentInSpace(ctx, agentdef.UpdateInput{AgentID: agent.ID, SpaceID: spaceID, UpdatedBy: userID,
			Def: agentdef.Definition{Name: name, Description: "d", Instructions: "i"}}); err != nil {
			t.Fatalf("UpdateAgentInSpace %s: %v", name, err)
		}
	}

	all, total, err := s.ListAgentRevisions(ctx, agent.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListAgentRevisions: %v", err)
	}
	if total != 3 || len(all) != 3 {
		t.Fatalf("total=%d len=%d, want 3 and 3", total, len(all))
	}
	if all[0].Name != "third" {
		t.Errorf("first row = %q, want the newest revision", all[0].Name)
	}

	page, total, err := s.ListAgentRevisions(ctx, agent.ID, 1, 1)
	if err != nil {
		t.Fatalf("ListAgentRevisions paged: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want the count before paging", total)
	}
	if len(page) != 1 || page[0].Name != "second" {
		t.Errorf("page = %+v, want one row holding the second revision", page)
	}
}

// TestAgentModelPersistsAndVersions asserts the model survives a round trip and
// that changing it appends a revision recording the old value, the same way the
// sandbox tiers do.
func TestAgentModelPersistsAndVersions(t *testing.T) {
	s, ctx := revisionTestStore(t)
	userID := newTestUser(t, s, "agentmodel")
	spaceID := newTestSpace(t, s, userID)

	created, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{SpaceID: spaceID, UserID: userID,
		Def: agentdef.Definition{Name: "picker", Description: "d", Instructions: "i", Model: "Fast"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	if created.Model != "Fast" {
		t.Fatalf("created model = %q, want Fast", created.Model)
	}

	got, err := s.GetAgent(ctx, created.ID)
	if err != nil || got == nil {
		t.Fatalf("GetAgent = %v, %v", got, err)
	}
	if got.Model != "Fast" {
		t.Errorf("read-back model = %q, want Fast", got.Model)
	}

	if _, err := s.UpdateAgentInSpace(ctx, agentdef.UpdateInput{AgentID: created.ID, SpaceID: spaceID, UpdatedBy: userID,
		Def: agentdef.Definition{Name: "picker", Description: "d", Instructions: "i", Model: "Deep"}}); err != nil {
		t.Fatalf("UpdateAgentInSpace: %v", err)
	}

	first, err := s.GetAgentRevision(ctx, created.ID, 1)
	if err != nil || first == nil {
		t.Fatalf("GetAgentRevision(1) = %v, %v", first, err)
	}
	if first.Model != "Fast" {
		t.Errorf("revision 1 model = %q, want Fast", first.Model)
	}
	second, err := s.GetAgentRevision(ctx, created.ID, 2)
	if err != nil || second == nil {
		t.Fatalf("GetAgentRevision(2) = %v, %v", second, err)
	}
	if second.Model != "Deep" {
		t.Errorf("revision 2 model = %q, want Deep", second.Model)
	}
}

func TestGetAgentRevisionReportsMissingAsNil(t *testing.T) {
	s, ctx := revisionTestStore(t)
	userID := newTestUser(t, s, "revision")
	spaceID := newTestSpace(t, s, userID)

	agent, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{SpaceID: spaceID, UserID: userID,
		Def: agentdef.Definition{Name: "only", Description: "d", Instructions: "i"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}

	got, err := s.GetAgentRevision(ctx, agent.ID, agent.Revision)
	if err != nil || got == nil {
		t.Fatalf("GetAgentRevision(existing) = %v, %v", got, err)
	}
	if got.Name != "only" {
		t.Errorf("Name = %q", got.Name)
	}

	missing, err := s.GetAgentRevision(ctx, agent.ID, 999)
	if err != nil {
		t.Fatalf("GetAgentRevision(missing) returned an error: %v", err)
	}
	if missing != nil {
		t.Errorf("a missing revision must read as nil, got %+v", missing)
	}
}

// The same two helpers, a different table and owner column.
func TestWorkflowRevisionsUseTheSameQueryShape(t *testing.T) {
	s, ctx := revisionTestStore(t)
	userID := newTestUser(t, s, "revision")
	spaceID := newTestSpace(t, s, userID)

	wf, err := s.CreateWorkflow(ctx, spaceID, userID, "wf", "d", `{"schema_version":1,"nodes":[]}`)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.WithContext(ctx).Delete(&workflowRevisionRow{}, "workflow_id = ?", wf.ID).Error
		_ = s.db.WithContext(ctx).Delete(&workflowRow{}, "workflow_id = ?", wf.ID).Error
	})

	list, total, err := s.ListWorkflowRevisions(ctx, wf.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListWorkflowRevisions: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len=%d, want 1 and 1", total, len(list))
	}

	got, err := s.GetWorkflowRevision(ctx, wf.ID, wf.Revision)
	if err != nil || got == nil {
		t.Fatalf("GetWorkflowRevision = %v, %v", got, err)
	}
	if got.Name != "wf" {
		t.Errorf("Name = %q", got.Name)
	}
	if missing, err := s.GetWorkflowRevision(ctx, wf.ID, 999); err != nil || missing != nil {
		t.Errorf("missing revision = %v, %v; want nil, nil", missing, err)
	}
}
