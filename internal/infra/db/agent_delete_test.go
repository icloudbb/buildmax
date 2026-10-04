package db

import (
	"errors"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
)

// Deleting an Agent marks it rather than removing it: live reads stop seeing
// it, a read that includes deleted Agents still resolves it for the Tasks and
// revisions that name it, and another Space cannot delete it.
func TestDeleteAgentInSpaceMarksItDeleted(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := newTestUser(t, s, "agentdel")
	space := newTestSpace(t, s, owner)
	elsewhere := newTestSpace(t, s, owner)
	a, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{SpaceID: space, UserID: owner, Def: agentdef.Definition{Name: "Doomed"}})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteAgentInSpace(ctx, a.ID, elsewhere); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("delete from another space = %v, want ErrNotFound", err)
	}
	if err := s.DeleteAgentInSpace(ctx, a.ID, space); err != nil {
		t.Fatalf("DeleteAgentInSpace: %v", err)
	}
	if got, err := s.GetAgent(ctx, a.ID); err != nil || got != nil {
		t.Fatalf("GetAgent after delete = %+v, %v; want nothing", got, err)
	}
	if got, err := s.GetAgentIncludingDeleted(ctx, a.ID); err != nil || got == nil {
		t.Fatalf("GetAgentIncludingDeleted = %+v, %v; want the marked row", got, err)
	}
	if list, _ := s.ListAgentsBySpace(ctx, space); len(list) != 0 {
		t.Errorf("listed after delete: %+v", list)
	}
	if err := s.DeleteAgentInSpace(ctx, a.ID, space); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("second delete = %v, want ErrNotFound", err)
	}
}
