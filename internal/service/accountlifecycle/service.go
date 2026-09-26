// Package accountlifecycle owns what happens to a person's durable footprint
// when a System Administrator disables or re-enables their account: the account
// gate commits first as the authority, then sessions, machine credentials,
// schedules, and in-flight runs are quieted, and the outcome is reported as
// counts separate from the gate result. Enabling reopens the gate and nothing
// else — revoked sessions, canceled runs, and paused schedules do not resurrect.
//
// It is the one place that sequences these stores, so a handler does not. See
// docs/design/system-administration.md §8.3.
package accountlifecycle

import (
	"context"
	"errors"
	"fmt"
	"slices"
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

// Cleanup steps a disable runs after the gate commits, as named in
// DisableResult.CleanupFailed.
const (
	StepSessions    = "sessions"
	StepWebhookKeys = "webhook_keys"
	StepSchedules   = "schedules"
	StepRuns        = "runs"
)

// ErrCleanupIncomplete marks a Disable whose gate committed but whose cleanup
// did not finish. The account is disabled, so a caller reports the state change
// with the failed steps rather than as a failed request.
var ErrCleanupIncomplete = errors.New("account disabled, but cleanup did not complete")

// DisableResult reports the gate outcome separately from the cleanup counts, so
// a timeout in cleanup cannot make an operator repeat or reverse the gate blindly.
type DisableResult struct {
	SessionsRevoked    int64 `json:"sessions_revoked"`
	WebhookKeysRetired int   `json:"webhook_keys_retired"`
	SchedulesPaused    int   `json:"schedules_paused"`
	RunsCanceled       int   `json:"runs_canceled"`
	// CleanupFailed names the steps that errored after the gate committed;
	// empty means cleanup completed. Disabling again is safe: it re-runs every
	// step, and each acts only on what is still live.
	CleanupFailed []string `json:"cleanup_failed,omitempty"`
}

// Disable commits the account gate, then quiets the account's sessions, machine
// credentials, schedules, and in-flight runs. The gate is the authority: it is
// set first, and a gate error is the only error that means the account was not
// disabled. A failing cleanup step does not stop the others — a broken schedule
// store must not leave runs uncanceled — and the result names every failed step
// alongside an error wrapping ErrCleanupIncomplete. The eligibility reconciler
// is the backstop for runs a failed step misses.
func (s *Service) Disable(ctx context.Context, userID string, opts DisableOptions) (DisableResult, error) {
	now := s.now()
	if err := s.Users.SetUserDisabled(ctx, userID, &now); err != nil {
		return DisableResult{}, err
	}
	var res DisableResult
	var errs []error
	fail := func(step string, err error) {
		if !slices.Contains(res.CleanupFailed, step) {
			res.CleanupFailed = append(res.CleanupFailed, step)
		}
		errs = append(errs, fmt.Errorf("%s: %w", step, err))
	}

	if s.Sessions != nil {
		n, err := s.Sessions.RevokeUserSessions(ctx, userID, now)
		if err != nil {
			fail(StepSessions, err)
		}
		res.SessionsRevoked = n
	}
	if opts.RetireWebhookKeys && s.Webhooks != nil {
		keys, err := s.Webhooks.ListKeys(ctx, userID)
		if err != nil {
			fail(StepWebhookKeys, err)
		}
		for _, k := range keys {
			if err := s.Webhooks.RevokeKey(ctx, userID, k.KeyID); err != nil {
				fail(StepWebhookKeys, err)
				continue
			}
			res.WebhookKeysRetired++
		}
	}
	if s.Schedules != nil {
		schedules, err := s.Schedules.ListEnabledSchedulesByCreator(ctx, userID)
		if err != nil {
			fail(StepSchedules, err)
		}
		disabled := false
		reason := coreschedule.PauseReasonCreatorDisabled
		for i := range schedules {
			if _, err := s.Schedules.UpdateSchedule(ctx, coreschedule.UpdateInput{
				ScheduleID: schedules[i].ID, Enabled: &disabled, PauseReason: &reason,
			}); err != nil {
				fail(StepSchedules, err)
				continue
			}
			res.SchedulesPaused++
		}
	}
	if s.Runs != nil {
		runs, err := s.Runs.ListActiveTaskRunsByCreator(ctx, userID)
		if err != nil {
			fail(StepRuns, err)
		}
		for i := range runs {
			// No requester: the deactivation, not a person, asked. The reason
			// carries why; the worker and the stale-run backstop settle it CANCELED.
			ok, err := s.Runs.RequestTaskRunCancel(ctx, runs[i].TaskRunID, "", coretask.CancelReasonCreatorDisabled, now)
			if err != nil {
				fail(StepRuns, err)
				continue
			}
			if ok {
				res.RunsCanceled++
			}
		}
	}
	if len(errs) > 0 {
		return res, fmt.Errorf("%w: %w", ErrCleanupIncomplete, errors.Join(errs...))
	}
	return res, nil
}

// Enable reopens the account gate and nothing else. Sessions, schedules, and
// runs quieted by a disable stay quieted; restoring them is a deliberate act, not
// a side effect of re-enabling.
func (s *Service) Enable(ctx context.Context, userID string) error {
	return s.Users.SetUserDisabled(ctx, userID, nil)
}
