// Package accountlifecycle owns what happens to a person's durable footprint
// when a System Administrator disables or re-enables their account: the account
// gate commits first as the authority, then sessions, machine credentials,
// schedules, and in-flight runs are quieted, and the outcome is reported as
// counts separate from the gate result. Enabling reopens the gate and nothing
// else — revoked sessions, canceled runs, and paused schedules do not resurrect.
//
// It is the one place that sequences these stores, so a handler does not. See
// docs/proposals/personnel-deactivation-lifecycle.md §8 and §10.
package accountlifecycle

import (
	"context"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// DefaultCancelBound is the reported upper bound on how long a run already under
// way keeps going after a deactivation asks it to stop: the worker's cancel-poll
// interval plus the stale-run backstop's grace. It is diagnostic — the actual
// stop is the worker's, honored on its next poll.
const DefaultCancelBound = 2*time.Minute + 5*time.Second

// UserGate reads and flips the deployment-wide account gate.
type UserGate interface {
	GetUser(ctx context.Context, userID string) (*coreidentity.User, error)
	SetUserDisabled(ctx context.Context, userID string, disabledAt *time.Time) error
}

// SessionStore revokes and counts a user's live sessions.
type SessionStore interface {
	RevokeUserSessions(ctx context.Context, userID string, now time.Time) (int64, error)
	CountUserSessions(ctx context.Context, userID string, now time.Time) (int, error)
}

// WebhookKeyStore lists and retires a user's webhook keys.
type WebhookKeyStore interface {
	ListKeys(ctx context.Context, userID string) ([]coreidentity.WebhookKeyMeta, error)
	RevokeKey(ctx context.Context, userID, keyID string) error
}

// ScheduleStore lists a creator's enabled schedules and pauses them.
type ScheduleStore interface {
	ListEnabledSchedulesByCreator(ctx context.Context, createdBy string) ([]coreschedule.Schedule, error)
	UpdateSchedule(ctx context.Context, in coreschedule.UpdateInput) (*coreschedule.Schedule, error)
}

// RunStore lists a creator's active runs and requests their cancellation.
type RunStore interface {
	ListActiveTaskRunsByCreator(ctx context.Context, createdBy string) ([]coretask.ActiveRunRef, error)
	RequestTaskRunCancel(ctx context.Context, taskRunID, requestedBy, reason string, requestedAt time.Time) (bool, error)
}

// SpaceStore reads a user's memberships and each Space's roster, for the impact
// projection and its sole-owner check.
type SpaceStore interface {
	ListSpacesByUser(ctx context.Context, userID string) ([]corespace.Space, error)
	ListSpaceMembers(ctx context.Context, spaceID string) ([]corespace.Member, error)
}

// Service sequences the account gate and its cleanup.
type Service struct {
	Users     UserGate
	Sessions  SessionStore
	Webhooks  WebhookKeyStore
	Schedules ScheduleStore
	Runs      RunStore
	Spaces    SpaceStore
	// CancelBound is the value the impact projection reports; zero uses
	// DefaultCancelBound.
	CancelBound time.Duration
	// Now is the clock, injectable for tests.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

// DisableOptions carries the operator's one choice: whether this is a temporary
// suspension that keeps the account's webhook keys for a deliberate return, or a
// leaver whose keys are retired for good.
type DisableOptions struct {
	RetireWebhookKeys bool
}

// DisableResult reports the gate outcome separately from the cleanup counts, so
// a timeout in cleanup cannot make an operator repeat or reverse the gate blindly.
type DisableResult struct {
	SessionsRevoked    int64 `json:"sessions_revoked"`
	WebhookKeysRetired int   `json:"webhook_keys_retired"`
	SchedulesPaused    int   `json:"schedules_paused"`
	RunsCanceled       int   `json:"runs_canceled"`
}

// Disable commits the account gate, then quiets the account's sessions, machine
// credentials, schedules, and in-flight runs. The gate is the authority: it is
// set first and returned even if a later cleanup step errors, so the account
// cannot act while cleanup converges (the eligibility reconciler is the backstop
// for anything a transient error here misses).
func (s *Service) Disable(ctx context.Context, userID string, opts DisableOptions) (DisableResult, error) {
	now := s.now()
	if err := s.Users.SetUserDisabled(ctx, userID, &now); err != nil {
		return DisableResult{}, err
	}
	var res DisableResult

	if s.Sessions != nil {
		n, err := s.Sessions.RevokeUserSessions(ctx, userID, now)
		if err != nil {
			return res, err
		}
		res.SessionsRevoked = n
	}
	if opts.RetireWebhookKeys && s.Webhooks != nil {
		keys, err := s.Webhooks.ListKeys(ctx, userID)
		if err != nil {
			return res, err
		}
		for _, k := range keys {
			if err := s.Webhooks.RevokeKey(ctx, userID, k.KeyID); err != nil {
				return res, err
			}
			res.WebhookKeysRetired++
		}
	}
	if s.Schedules != nil {
		schedules, err := s.Schedules.ListEnabledSchedulesByCreator(ctx, userID)
		if err != nil {
			return res, err
		}
		disabled := false
		reason := coreschedule.PauseReasonCreatorDisabled
		for i := range schedules {
			if _, err := s.Schedules.UpdateSchedule(ctx, coreschedule.UpdateInput{
				ScheduleID: schedules[i].ID, Enabled: &disabled, PauseReason: &reason,
			}); err != nil {
				return res, err
			}
			res.SchedulesPaused++
		}
	}
	if s.Runs != nil {
		runs, err := s.Runs.ListActiveTaskRunsByCreator(ctx, userID)
		if err != nil {
			return res, err
		}
		for i := range runs {
			// No requester: the deactivation, not a person, asked. The reason
			// carries why; the worker and the stale-run backstop settle it CANCELED.
			ok, err := s.Runs.RequestTaskRunCancel(ctx, runs[i].TaskRunID, "", coretask.CancelReasonCreatorDisabled, now)
			if err != nil {
				return res, err
			}
			if ok {
				res.RunsCanceled++
			}
		}
	}
	return res, nil
}

// Enable reopens the account gate and nothing else. Sessions, schedules, and
// runs quieted by a disable stay quieted; restoring them is a deliberate act, not
// a side effect of re-enabling.
func (s *Service) Enable(ctx context.Context, userID string) error {
	return s.Users.SetUserDisabled(ctx, userID, nil)
}
