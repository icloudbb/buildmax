package accountlifecycle

import (
	"context"
	"time"

	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// Impact is the read-only projection an operator reads before disabling an
// account: what stops, what needs a successor, and how long in-flight work may
// run. It is metadata and counts only — no prompt, input, output, Artifact name,
// trace, or Secret — so learning the shape of an account's footprint never leaks
// Space content.
type Impact struct {
	LiveSessions       int             `json:"live_sessions"`
	WebhookKeys        int             `json:"webhook_keys"`
	Memberships        []MembershipRef `json:"memberships"`
	SoleOwnedSpaceIDs  []string        `json:"sole_owned_space_ids"`
	EnabledSchedules   int             `json:"enabled_schedules"`
	ActiveRunsByStatus map[string]int  `json:"active_runs_by_status"`
	CancellationBound  string          `json:"cancellation_bound"`
}

// MembershipRef is one Space the account belongs to and the role it holds there.
type MembershipRef struct {
	SpaceID string `json:"space_id"`
	Role    string `json:"role"`
}

// Impact computes the projection. Every field is a count or an id; nothing here
// reads a Space's contents.
func (s *Service) Impact(ctx context.Context, userID string) (Impact, error) {
	out := Impact{
		ActiveRunsByStatus: map[string]int{},
		Memberships:        []MembershipRef{},
		SoleOwnedSpaceIDs:  []string{},
		CancellationBound:  s.cancelBound().String(),
	}

	if s.Sessions != nil {
		n, err := s.Sessions.CountUserSessions(ctx, userID, s.now())
		if err != nil {
			return Impact{}, err
		}
		out.LiveSessions = n
	}
	if s.Webhooks != nil {
		keys, err := s.Webhooks.ListKeys(ctx, userID)
		if err != nil {
			return Impact{}, err
		}
		out.WebhookKeys = len(keys)
	}
	if s.Spaces != nil {
		spaces, err := s.Spaces.ListSpacesByUser(ctx, userID)
		if err != nil {
			return Impact{}, err
		}
		for i := range spaces {
			members, err := s.Spaces.ListSpaceMembers(ctx, spaces[i].ID)
			if err != nil {
				return Impact{}, err
			}
			role := corespace.EffectiveRoleOf(members, userID)
			if role == "" {
				continue
			}
			out.Memberships = append(out.Memberships, MembershipRef{SpaceID: spaces[i].ID, Role: role})
			if role == corespace.RoleOwner && ownerCount(members) == 1 {
				out.SoleOwnedSpaceIDs = append(out.SoleOwnedSpaceIDs, spaces[i].ID)
			}
		}
	}
	if s.Schedules != nil {
		schedules, err := s.Schedules.ListEnabledSchedulesByCreator(ctx, userID)
		if err != nil {
			return Impact{}, err
		}
		out.EnabledSchedules = len(schedules)
	}
	if s.Runs != nil {
		runs, err := s.Runs.ListActiveTaskRunsByCreator(ctx, userID)
		if err != nil {
			return Impact{}, err
		}
		for i := range runs {
			out.ActiveRunsByStatus[runs[i].Status]++
		}
	}
	return out, nil
}

func (s *Service) cancelBound() time.Duration {
	if s.CancelBound > 0 {
		return s.CancelBound
	}
	return DefaultCancelBound
}

func ownerCount(members []corespace.Member) int {
	n := 0
	for i := range members {
		if corespace.EffectiveRole(members[i].Role) == corespace.RoleOwner {
			n++
		}
	}
	return n
}
