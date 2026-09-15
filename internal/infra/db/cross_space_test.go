package db

import (
	"errors"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

// Space is the authorization boundary for Portal resources, and these store
// methods scope by it themselves rather than trusting the caller. The handler
// role-matrix tests already prove the edge rejects a cross-space request; these
// prove the store's own scoping so a leak cannot slip in below the handlers.
// Each case gives the operation a request that would succeed in the owning
// space — the real revision, the real version, an existing plugin — so what
// rejects it is provably the space guard and not an incidental version or
// existence miss.

// TestUpdateWorkflowInAForeignSpaceIsANoOp proves a workflow update addressed
// from another space neither mutates the workflow nor reveals that it exists:
// the call answers exactly as it would for an id that names nothing.
func TestUpdateWorkflowInAForeignSpaceIsANoOp(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "r2-wf-owner")
	ownerSpace := newTestSpace(t, s, owner)
	stranger := newTestUser(t, s, "r2-wf-stranger")
	strangerSpace := newTestSpace(t, s, stranger)

	wf, err := s.CreateWorkflow(ctx, ownerSpace, owner, "owned", "the owner's plan", `{"schema_version":1,"nodes":[]}`)
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// The correct in-space revision, so only the space guard can reject this.
	hijack := "hijacked"
	got, err := s.UpdateWorkflow(ctx, wf.ID, strangerSpace, coreworkflow.UpdateInput{
		Name: &hijack, ExpectedRevision: 1, UpdatedBy: stranger,
	})
	if err != nil || got != nil {
		t.Fatalf("cross-space UpdateWorkflow = (%v, %v), want (nil, nil)", got, err)
	}

	// A foreign real handle must answer the same as one that names nothing.
	unknown, err := util.NewPublicID()
	if err != nil {
		t.Fatalf("NewPublicID: %v", err)
	}
	if got, err := s.UpdateWorkflow(ctx, unknown, strangerSpace, coreworkflow.UpdateInput{
		Name: &hijack, ExpectedRevision: 1, UpdatedBy: stranger,
	}); err != nil || got != nil {
		t.Fatalf("unknown UpdateWorkflow = (%v, %v), want (nil, nil) — a foreign handle must be indistinguishable from an unknown one", got, err)
	}

	// The owner's workflow kept its name and revision: nothing leaked through.
	after, err := s.GetWorkflow(ctx, wf.ID)
	if err != nil || after == nil {
		t.Fatalf("GetWorkflow after the foreign update: (%v, %v)", after, err)
	}
	if after.Name != "owned" {
		t.Errorf("workflow name = %q, want %q — a foreign space rewrote it", after.Name, "owned")
	}
	if after.Revision != wf.Revision {
		t.Errorf("workflow revision moved %d -> %d under a foreign update", wf.Revision, after.Revision)
	}
}

// TestUpdateIssueInAForeignSpaceIsANoOp proves the same for the issue space
// guard: an update from another space is a silent no-op, not a write.
func TestUpdateIssueInAForeignSpaceIsANoOp(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "r2-issue-owner")
	ownerSpace := newTestSpace(t, s, owner)
	stranger := newTestUser(t, s, "r2-issue-stranger")
	strangerSpace := newTestSpace(t, s, stranger)

	issue, err := s.CreateIssueInSpace(ctx, ownerSpace, owner, coreissue.CreateInput{Title: "owned"})
	if err != nil {
		t.Fatalf("CreateIssueInSpace: %v", err)
	}

	// The correct in-space version, so only the space guard can reject this.
	hijack := "hijacked"
	got, err := s.UpdateIssueInSpace(ctx, issue.ID, strangerSpace, coreissue.UpdateInput{
		IfVersion: issue.Version, Title: &hijack,
	})
	if err != nil || got != nil {
		t.Fatalf("cross-space UpdateIssueInSpace = (%v, %v), want (nil, nil)", got, err)
	}

	after, err := s.GetIssue(ctx, issue.ID)
	if err != nil || after == nil {
		t.Fatalf("GetIssue after the foreign update: (%v, %v)", after, err)
	}
	if after.Title != "owned" {
		t.Errorf("issue title = %q, want %q — a foreign space rewrote it", after.Title, "owned")
	}
	if after.Version != issue.Version {
		t.Errorf("issue version moved %d -> %d under a foreign update", issue.Version, after.Version)
	}
}

// TestPluginActivationIsScopedToItsSpace proves an activation belongs to the
// space that made it: another space cannot read it, list it, or suspend it.
func TestPluginActivationIsScopedToItsSpace(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "r2-plugin-owner")
	ownerSpace := newTestSpace(t, s, owner)
	stranger := newTestUser(t, s, "r2-plugin-stranger")
	strangerSpace := newTestSpace(t, s, stranger)

	if _, err := s.ActivatePlugin(ctx, coreplugin.ActivateInput{
		SpaceID:    ownerSpace,
		PluginName: "code-review",
		Version:    "1.0.0",
		Digest:     "sha256:one",
		Origin:     coreplugin.ActivationCurated,
		ActorID:    owner,
	}); err != nil {
		t.Fatalf("ActivatePlugin: %v", err)
	}

	// The stranger's space names the same plugin but has not activated it.
	if got, err := s.GetPluginActivation(ctx, strangerSpace, "code-review"); err != nil || got != nil {
		t.Fatalf("GetPluginActivation(stranger) = (%v, %v), want (nil, nil)", got, err)
	}
	if listed, err := s.ListPluginActivations(ctx, strangerSpace); err != nil || len(listed) != 0 {
		t.Fatalf("ListPluginActivations(stranger) = (%d, %v), want (0, nil)", len(listed), err)
	}

	// Suspending it from the stranger's space finds nothing to suspend rather
	// than reaching across the boundary.
	if _, err := s.SetPluginActivationEnabled(ctx, strangerSpace, "code-review", false, stranger); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("cross-space SetPluginActivationEnabled err = %v, want ErrNotFound", err)
	}

	// The owner's activation is untouched: still there and still enabled.
	after, err := s.GetPluginActivation(ctx, ownerSpace, "code-review")
	if err != nil || after == nil {
		t.Fatalf("GetPluginActivation(owner) after the foreign suspend: (%v, %v)", after, err)
	}
	if !after.Enabled {
		t.Errorf("owner's activation was suspended by another space")
	}
}
