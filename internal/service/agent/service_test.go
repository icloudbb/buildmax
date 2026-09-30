package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/agent"
)

func newService(t *testing.T) (*agent.Service, *mock.MockAgentStore, context.Context) {
	t.Helper()
	store := &mock.MockAgentStore{}
	return &agent.Service{Agents: store}, store, context.Background()
}

func TestCreateAgentSharesSpaceInstructionBudget(t *testing.T) {
	svc, store, ctx := newService(t)
	svc.Spaces = &mock.MockSpaceStore{Spaces: []corespace.Space{{
		ID: "tm_1", AgentInstructions: strings.Repeat("s", 4096),
	}}}
	_, err := svc.CreateAgent(ctx, agent.CreateCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "writer", Instructions: strings.Repeat("a", 4097),
	})
	if !errors.Is(err, agent.ErrInstructionsTooLong) {
		t.Fatalf("err = %v, want ErrInstructionsTooLong", err)
	}
	if len(store.Agents) != 0 {
		t.Fatal("over-budget agent was stored")
	}
}

func create(t *testing.T, s *agent.Service, spaceID string) *agentdef.Agent {
	t.Helper()
	a, err := s.CreateAgent(context.Background(), agent.CreateCmd{
		SpaceID: spaceID, UserID: "u_1", Name: "reviewer", Description: "d", Instructions: "i",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	return a
}

func TestCreateRequiresAName(t *testing.T) {
	s, _, ctx := newService(t)

	_, err := s.CreateAgent(ctx, agent.CreateCmd{SpaceID: "tm_1", UserID: "u_1"})

	if !errors.Is(err, agent.ErrNameRequired) {
		t.Fatalf("err = %v, want ErrNameRequired", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindInvalid {
		t.Errorf("kind = %q, want invalid", kind)
	}
}

// TestCreateRejectsUnknownSandboxTier asserts a tier outside
// config.ValidSandboxNetworkTier/ValidSandboxFilesystemTier is refused before
// anything is stored, the same way an empty name is.
func TestCreateRejectsUnknownSandboxTier(t *testing.T) {
	s, _, ctx := newService(t)

	_, err := s.CreateAgent(ctx, agent.CreateCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "reviewer",
		SandboxNetworkTier: "unlimited",
	})

	if !errors.Is(err, agent.ErrInvalidSandboxTier) {
		t.Fatalf("err = %v, want ErrInvalidSandboxTier", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindInvalid {
		t.Errorf("kind = %q, want invalid", kind)
	}
}

// TestCreateAndUpdateStoreSandboxTiers asserts a valid declared tier is
// stored on create, versions with an update, and an update that omits it
// resets that axis to the strictest tier rather than leaving it unchanged.
func TestCreateAndUpdateStoreSandboxTiers(t *testing.T) {
	s, _, ctx := newService(t)

	a, err := s.CreateAgent(ctx, agent.CreateCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "builder",
		SandboxNetworkTier: "registries", SandboxFilesystemTier: "workspace",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if a.SandboxNetworkTier != "registries" {
		t.Errorf("SandboxNetworkTier = %q, want registries", a.SandboxNetworkTier)
	}

	updated, err := s.UpdateAgent(ctx, agent.UpdateCmd{
		SpaceID: "tm_1", UserID: "u_1", AgentID: a.ID, Name: "builder",
	})
	if err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}
	if updated.SandboxNetworkTier != "" {
		t.Errorf("SandboxNetworkTier after omitting it = %q, want empty (reset to strictest)", updated.SandboxNetworkTier)
	}
}

// The ownership check was written out separately in four handlers. It belongs
// in one place, and it has to answer not-found rather than forbidden so the
// reply does not confirm that the id exists in another space.
func TestAnotherSpacesAgentReadsAsNotFound(t *testing.T) {
	s, _, ctx := newService(t)
	other := create(t, s, "tm_other")

	_, err := s.GetAgent(ctx, "tm_mine", other.ID)

	if !errors.Is(err, agent.ErrAgentNotFound) {
		t.Fatalf("err = %v, want ErrAgentNotFound", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindNotFound {
		t.Errorf("kind = %q, want not_found", kind)
	}
}

func TestRevisionsAndRestoreStayInsideTheSpace(t *testing.T) {
	s, _, ctx := newService(t)
	other := create(t, s, "tm_other")

	if _, _, err := s.ListRevisions(ctx, "tm_mine", other.ID, 10, 0); !errors.Is(err, agent.ErrAgentNotFound) {
		t.Errorf("ListRevisions leaked another space's agent: %v", err)
	}
	_, err := s.RestoreRevision(ctx, agent.RestoreRevisionCmd{SpaceID: "tm_mine", UserID: "u_1", AgentID: other.ID, Revision: 1})
	if !errors.Is(err, agent.ErrAgentNotFound) {
		t.Errorf("RestoreRevision leaked another space's agent: %v", err)
	}
}

// Restoring is an edit: it appends a revision rather than rewinding history.
func TestRestoreAppendsRatherThanRewinds(t *testing.T) {
	s, _, ctx := newService(t)
	a := create(t, s, "tm_1")
	if _, err := s.UpdateAgent(ctx, agent.UpdateCmd{
		SpaceID: "tm_1", UserID: "u_1", AgentID: a.ID, Name: "renamed", Description: "d2", Instructions: "i2",
	}); err != nil {
		t.Fatalf("UpdateAgent: %v", err)
	}

	restored, err := s.RestoreRevision(ctx, agent.RestoreRevisionCmd{
		SpaceID: "tm_1", UserID: "u_1", AgentID: a.ID, Revision: 1,
	})
	if err != nil {
		t.Fatalf("RestoreRevision: %v", err)
	}
	if restored.Name != "reviewer" {
		t.Errorf("Name = %q, want the first revision's name back", restored.Name)
	}

	_, total, err := s.ListRevisions(ctx, "tm_1", a.ID, 0, 0)
	if err != nil {
		t.Fatalf("ListRevisions: %v", err)
	}
	if total != 3 {
		t.Errorf("revisions = %d, want 3: create, rename, restore", total)
	}
}

func TestMissingRevisionIsReported(t *testing.T) {
	s, _, ctx := newService(t)
	a := create(t, s, "tm_1")

	_, err := s.RestoreRevision(ctx, agent.RestoreRevisionCmd{
		SpaceID: "tm_1", UserID: "u_1", AgentID: a.ID, Revision: 999,
	})

	if !errors.Is(err, agent.ErrRevisionNotFound) {
		t.Fatalf("err = %v, want ErrRevisionNotFound", err)
	}
}

type usedBy []coreworkflow.Workflow

func (u usedBy) PublishedWorkflowsUsingAgent(context.Context, string, string) ([]coreworkflow.Workflow, error) {
	return u, nil
}

// Deleting an agent a published workflow names would break that workflow at its
// next step, so the refusal names the workflows rather than just saying no.
func TestDeleteNamesTheWorkflowsBlockingIt(t *testing.T) {
	s, _, ctx := newService(t)
	a := create(t, s, "tm_1")
	s.Workflows = usedBy{{ID: "w_1", Name: "nightly"}, {ID: "w_2", Name: "release"}}

	err := s.DeleteAgent(ctx, "tm_1", a.ID, "u_1")

	if !errors.Is(err, agent.ErrUsedByPublishedFlows) {
		t.Fatalf("err = %v, want ErrUsedByPublishedFlows", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindConflict {
		t.Errorf("kind = %q, want conflict", kind)
	}
	for _, want := range []string{"nightly (w_1)", "release (w_2)"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("want %q named in %q", want, err.Error())
		}
	}
}

// A deployment that cannot answer the question does not block the delete, which
// is what the handler did when the store was nil.
func TestDeleteProceedsWithoutAWorkflowSource(t *testing.T) {
	s, _, ctx := newService(t)
	a := create(t, s, "tm_1")

	if err := s.DeleteAgent(ctx, "tm_1", a.ID, "u_1"); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if _, err := s.GetAgent(ctx, "tm_1", a.ID); !errors.Is(err, agent.ErrAgentNotFound) {
		t.Errorf("a deleted agent should read as not found, got %v", err)
	}
}

func TestDeletingAnotherSpacesAgentIsNotFound(t *testing.T) {
	s, _, ctx := newService(t)
	other := create(t, s, "tm_other")

	if err := s.DeleteAgent(ctx, "tm_mine", other.ID, "u_1"); !errors.Is(err, agent.ErrAgentNotFound) {
		t.Errorf("err = %v, want ErrAgentNotFound", err)
	}
}

// Every method has to say "not configured" rather than panic on a deployment
// with no database.
func TestNoStoreIsReportedNotPanicked(t *testing.T) {
	s := &agent.Service{}
	ctx := context.Background()

	checks := []error{}
	_, err := s.ListAgents(ctx, "tm_1")
	checks = append(checks, err)
	_, err = s.CreateAgent(ctx, agent.CreateCmd{SpaceID: "tm_1", Name: "x"})
	checks = append(checks, err)
	_, err = s.GetAgent(ctx, "tm_1", "a_1")
	checks = append(checks, err)
	_, err = s.UpdateAgent(ctx, agent.UpdateCmd{SpaceID: "tm_1", AgentID: "a_1", Name: "x"})
	checks = append(checks, err)
	checks = append(checks, s.DeleteAgent(ctx, "tm_1", "a_1", "u_1"))

	for i, err := range checks {
		if !errors.Is(err, agent.ErrAgentsNotConfigured) {
			t.Errorf("check %d: err = %v, want ErrAgentsNotConfigured", i, err)
		}
	}
}

// fakeModelCatalog answers a fixed set of model names, and optionally an error,
// so validateModel's paths can be exercised without a gateway.
type fakeModelCatalog struct {
	names []string
	err   error
}

func (f fakeModelCatalog) ModelNames(context.Context) ([]string, error) {
	return f.names, f.err
}

// TestCreateRejectsUnknownModel asserts a model the deployment's catalog does
// not list is refused before anything is stored, the same way an unknown
// sandbox tier is.
func TestCreateRejectsUnknownModel(t *testing.T) {
	s, _, ctx := newService(t)
	s.Models = fakeModelCatalog{names: []string{"Fast", "Deep"}}

	_, err := s.CreateAgent(ctx, agent.CreateCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "reviewer", Model: "Nonesuch",
	})

	if !errors.Is(err, agent.ErrUnknownModel) {
		t.Fatalf("err = %v, want ErrUnknownModel", err)
	}
	if kind, _ := apierr.KindOf(err); kind != apierr.KindInvalid {
		t.Errorf("kind = %q, want invalid", kind)
	}
}

// TestCreateAcceptsAKnownModel stores a model the catalog lists, trimmed.
func TestCreateAcceptsAKnownModel(t *testing.T) {
	s, _, ctx := newService(t)
	s.Models = fakeModelCatalog{names: []string{"Fast", "Deep"}}

	a, err := s.CreateAgent(ctx, agent.CreateCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "reviewer", Model: "  Fast  ",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if a.Model != "Fast" {
		t.Errorf("Model = %q, want trimmed Fast", a.Model)
	}
}

// TestCreateAcceptsAnEmptyModel is the deployment default: no name, no catalog
// call, no refusal.
func TestCreateAcceptsAnEmptyModel(t *testing.T) {
	s, _, ctx := newService(t)
	s.Models = fakeModelCatalog{err: errors.New("catalog must not be consulted for an empty model")}

	a, err := s.CreateAgent(ctx, agent.CreateCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "reviewer",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if a.Model != "" {
		t.Errorf("Model = %q, want empty", a.Model)
	}
}

// TestCreateStoresModelUncheckedWithoutACatalog covers the direct-transport
// deployment: no catalog is wired, so a model name is stored as given rather
// than refused.
func TestCreateStoresModelUncheckedWithoutACatalog(t *testing.T) {
	s, _, ctx := newService(t)

	a, err := s.CreateAgent(ctx, agent.CreateCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "reviewer", Model: "whatever",
	})
	if err != nil {
		t.Fatalf("CreateAgent: %v", err)
	}
	if a.Model != "whatever" {
		t.Errorf("Model = %q, want whatever", a.Model)
	}
}

// A name over its column cap or whitespace-only is a clean invalid error, not a
// write failure surfaced as a 500.
func TestCreateAgent_NameBounds(t *testing.T) {
	svc, _, ctx := newService(t)
	base := agent.CreateCmd{SpaceID: "tm_1", UserID: "u1", Instructions: "do"}
	long := base
	long.Name = strings.Repeat("x", 256)
	if _, err := svc.CreateAgent(ctx, long); err != agent.ErrNameTooLong {
		t.Errorf("over-long name err = %v, want ErrNameTooLong", err)
	}
	ws := base
	ws.Name = "   \n\t"
	if _, err := svc.CreateAgent(ctx, ws); err != agent.ErrNameRequired {
		t.Errorf("whitespace name err = %v, want ErrNameRequired", err)
	}
	ok := base
	ok.Name = strings.Repeat("y", 255)
	if _, err := svc.CreateAgent(ctx, ok); err != nil {
		t.Errorf("name at the cap should be accepted: %v", err)
	}
}
