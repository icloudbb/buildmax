package quota

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
)

// quotaWarnThreshold is the share of a limit that turns into a recorded
// warning.
//
// It is deliberately a single number rather than a ladder of them. For a
// trusted space quota is cost control, not a security boundary, and a trail that
// says a space passed 50%, 75%, 80% and 90% of the same limit in one period is
// four rows saying what one row said first.
const quotaWarnThreshold = 0.8

// alertLookback bounds the dedupe read. A space that submits work continuously
// still writes at most one warning per limit per period, and finding the
// existing one only requires looking at that period's events for this action.
const alertLookback = 50

// A storage limit's events say what the space holds now rather than what it
// spent in a window. The dedupe still uses the tier's period, which bounds how
// often a space sitting at its storage limit writes the same row.

// noteUsage records that a space crossed a share of its quota, or was refused by
// it, at most once per limit per period.
//
// Both events are written from the admission path because that is the only
// place that knows the answer: usage is computed against a rolling window, so
// there is no periodic boundary at which a sweep could notice a space is at 80%.
// The cost is a read and a write on an admission that is already at the
// threshold, and neither is allowed to change the admission decision — a
// deployment whose audit table is unreachable must still run work.
func (c *Service) noteUsage(ctx context.Context, spaceID string, limit corequota.Limit, used, max int64, windowStart time.Time, denied bool) {
	if c.Audit == nil || max <= 0 {
		return
	}
	action := coreaudit.QuotaThresholdReached
	detail := fmt.Sprintf("%s %d%% of %d", limit, int(float64(used)*100/float64(max)), max)
	if denied {
		action = coreaudit.QuotaExceeded
		detail = fmt.Sprintf("%s limit reached at %d of %d", limit, used, max)
	}

	if c.alreadyNoted(ctx, spaceID, action, string(limit), windowStart) {
		return
	}
	if err := c.Audit.RecordAuditEvent(ctx, coreaudit.Event{
		SpaceID: spaceID,
		// The actor is the system, not whoever submitted the work that tipped
		// the total over. A quota is a property of the space, and naming the
		// last member to submit would read as blame for a shared budget.
		ActorType:  coreaudit.ActorSystem,
		ActorID:    coreaudit.ActorOperator,
		Action:     action,
		TargetType: "space",
		TargetID:   spaceID,
		Detail:     detail,
	}); err != nil {
		slog.Warn("quota event not recorded", "err", err, "space_id", spaceID, "action", action)
	}
}

// alreadyNoted reports whether this action was already recorded for this limit
// in the current period.
//
// It matches on the detail's leading term rather than on the whole string,
// because the detail carries the numbers as they were at the time and a second
// crossing would produce different ones. Failing to read is treated as "already
// noted": a duplicate warning is noise, and noise in an evidence table is worse
// than a warning that was skipped once because the database was busy.
func (c *Service) alreadyNoted(ctx context.Context, spaceID, action, limit string, windowStart time.Time) bool {
	events, _, err := c.Audit.SearchAuditEvents(ctx, coreaudit.Filter{
		SpaceID: spaceID,
		Action:  action,
		Since:   windowStart,
	}, alertLookback, 0)
	if err != nil {
		slog.Warn("quota event dedupe read failed", "err", err, "space_id", spaceID, "action", action)
		return true
	}
	for _, event := range events {
		if strings.HasPrefix(event.Detail, limit) {
			return true
		}
	}
	return false
}
