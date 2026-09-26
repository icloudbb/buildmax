package conversation

import (
	"context"
	"errors"
	"testing"

	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
)

func TestListSpacesMarksPersonalAndCurrent(t *testing.T) {
	owner := "u_1"
	store := &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: "s_personal", Name: "My Space", PersonalForUserID: &owner},
			{ID: "s_qa", Name: "QA"},
		},
		Members: []corespace.Member{
			{SpaceID: "s_personal", UserID: owner},
			{SpaceID: "s_qa", UserID: owner},
		},
	}
	out, err := newListSpacesTool(store, owner, "s_qa").Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "1. My Space (personal)\n2. QA ← current"; out != want {
		t.Errorf("Execute = %q, want %q", out, want)
	}
}

func TestListSpacesWithoutMembership(t *testing.T) {
	out, err := newListSpacesTool(&mock.MockSpaceStore{}, "u_1", "").Execute(context.Background(), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "The user belongs to no Space." {
		t.Errorf("Execute = %q", out)
	}
}

func TestListSpacesReportsStoreFailure(t *testing.T) {
	boom := errors.New("db down")
	_, err := newListSpacesTool(failingSpaceLister{boom}, "u_1", "").Execute(context.Background(), nil)
	if !errors.Is(err, boom) {
		t.Fatalf("Execute error = %v, want %v", err, boom)
	}
}

func TestListSpacesNotConfigured(t *testing.T) {
	_, err := newListSpacesTool(nil, "u_1", "").Execute(context.Background(), nil)
	if err == nil || err.Error() != "ListSpaces not configured" {
		t.Fatalf("Execute error = %v, want ListSpaces not configured", err)
	}
}

type failingSpaceLister struct{ err error }

func (f failingSpaceLister) ListSpacesByUser(context.Context, string) ([]corespace.Space, error) {
	return nil, f.err
}
