package quota

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
)

var (
	ErrSpaceNotFound = apierr.New(apierr.KindNotFound, "space not found")
	ErrUnknownTier   = apierr.New(apierr.KindInvalid, "unknown quota tier")
)

// ListTiers returns every defined tier, ordered by name. Tiers are seeded, not
// edited, so this is the whole set a space can be assigned to.
func (c *Service) ListTiers(ctx context.Context) ([]corequota.Tier, error) {
	tiers, err := c.TierStore.ListQuotaTiers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list quota tiers: %w", err)
	}
	return tiers, nil
}

// AssignTier moves a space onto an existing tier on behalf of a System
// Administrator, and returns the tier the space ran under before.
//
// Nothing is stopped or re-checked: Check reads the space's tier on every
// admission, so the change applies from the next one, and work already admitted
// keeps running. Assigning the tier the space already records is a no-op and is
// not audited. The audit write is best-effort after the change commits, like
// every other audit write.
func (c *Service) AssignTier(ctx context.Context, actorID, spaceID, tierName string) (previous string, err error) {
	tierName = strings.TrimSpace(tierName)
	space, err := c.SpaceStore.GetSpace(ctx, spaceID)
	if err != nil {
		return "", fmt.Errorf("read space %s: %w", spaceID, err)
	}
	if space == nil {
		return "", ErrSpaceNotFound
	}
	tiers, err := c.ListTiers(ctx)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(tiers))
	known := false
	for _, tier := range tiers {
		names = append(names, tier.TierName)
		known = known || tier.TierName == tierName
	}
	if !known {
		// Name the choices: the operator who mistyped one should not need a
		// second call to learn what exists.
		valid := strings.Join(names, ", ")
		if valid == "" {
			valid = "none are defined"
		}
		return "", apierr.Detail(ErrUnknownTier, "%q; valid tiers: %s", tierName, valid)
	}

	previous = space.QuotaTier
	if previous == "" {
		previous = c.DefaultTier
	}
	if space.QuotaTier == tierName {
		return previous, nil
	}
	if err := c.SpaceStore.SetSpaceQuotaTier(ctx, spaceID, tierName); err != nil {
		if errors.Is(err, apierr.ErrNotFound) {
			return "", ErrSpaceNotFound
		}
		return "", fmt.Errorf("set space %s quota tier: %w", spaceID, err)
	}
	if c.Audit != nil {
		if err := c.Audit.RecordAuditEvent(ctx, coreaudit.Event{
			SpaceID:    spaceID,
			ActorType:  coreaudit.ActorUser,
			ActorID:    actorID,
			Action:     coreaudit.SpaceQuotaTierChanged,
			TargetType: "space",
			TargetID:   spaceID,
			Detail:     previous + " -> " + tierName,
		}); err != nil {
			slog.Error("audit event not recorded", "err", err,
				"action", coreaudit.SpaceQuotaTierChanged, "actor_id", actorID, "space_id", spaceID)
		}
	}
	return previous, nil
}
