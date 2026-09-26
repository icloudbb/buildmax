package db

import (
	"context"
	"os"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agentdef"
)

func TestAgentStore_SecretConsumptionRoundTrip(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID, spaceID := secretTestSpace(t, s, "agent-consumption@example.com")

	cons := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{
		{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"},
		{Secret: "sec_aws", Prefix: "AWS_"},
	}}
	created, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{
		SpaceID: spaceID,
		UserID:  userID,
		Def:     agentdef.Definition{Name: "deployer", SecretConsumption: cons},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	t.Cleanup(func() {
		s.db.Where("agent_id IN (SELECT id FROM agent WHERE public_id = ?)", created.ID).Delete(&agentRevisionRow{})
		s.db.Where("public_id = ?", created.ID).Delete(&agentRow{})
	})

	// The live agent carries it.
	got, err := s.GetAgent(ctx, created.ID)
	if err != nil || got == nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if !got.SecretConsumption.Equal(cons) {
		t.Fatalf("agent consumption = %+v, want %+v", got.SecretConsumption, cons)
	}

	// So does revision 1.
	rev1, err := s.GetAgentRevision(ctx, created.ID, 1)
	if err != nil || rev1 == nil {
		t.Fatalf("GetAgentRevision 1: %v", err)
	}
	if !rev1.SecretConsumption.Equal(cons) {
		t.Fatalf("revision 1 consumption = %+v", rev1.SecretConsumption)
	}

	// Reordering the same grants is not an edit: no new revision.
	reordered := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{
		{Secret: "sec_aws", Prefix: "AWS_"},
		{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"},
	}}
	afterNoop, err := s.UpdateAgentInSpace(ctx, agentdef.UpdateInput{
		AgentID: created.ID, SpaceID: spaceID, UpdatedBy: userID,
		Def: agentdef.Definition{Name: "deployer", SecretConsumption: reordered},
	})
	if err != nil {
		t.Fatalf("UpdateAgentInSpace (no-op): %v", err)
	}
	if afterNoop.Revision != 1 {
		t.Fatalf("reordering appended a revision: now at %d", afterNoop.Revision)
	}

	// A real change appends a revision that carries the new consumption, while
	// revision 1 still answers the old.
	changed := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{
		{Secret: "sec_gh", Item: "token", EnvName: "GITHUB_TOKEN"},
	}}
	after, err := s.UpdateAgentInSpace(ctx, agentdef.UpdateInput{
		AgentID: created.ID, SpaceID: spaceID, UpdatedBy: userID,
		Def: agentdef.Definition{Name: "deployer", SecretConsumption: changed},
	})
	if err != nil {
		t.Fatalf("UpdateAgentInSpace: %v", err)
	}
	if after.Revision != 2 || !after.SecretConsumption.Equal(changed) {
		t.Fatalf("after change: rev=%d cons=%+v", after.Revision, after.SecretConsumption)
	}
	rev1Again, err := s.GetAgentRevision(ctx, created.ID, 1)
	if err != nil {
		t.Fatalf("GetAgentRevision 1 again: %v", err)
	}
	if !rev1Again.SecretConsumption.Equal(cons) {
		t.Fatal("revision 1 consumption drifted after a later edit")
	}
}
