package agent

import (
	"context"
	"errors"
	"log/slog"
	"slices"

	"github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// SecretLookup answers what Secret consumption validation needs: a space's
// Secret and its item names. It is an interface, and not the secret store
// itself, for the reason PluginSelection is one: an agent edit needs one
// question answered and must not depend on secret cryptography or lifecycle.
type SecretLookup interface {
	GetSecret(ctx context.Context, id string) (*coresecret.Secret, error)
}

// validateConsumption checks an agent's Secret consumption against the space's
// live Secrets before the definition is stored -- refused while somebody is
// watching a create/update, the same as a plugin selection or a sandbox tier.
// The run resolves values again from the pinned revision; this catches a config
// that could never work.
//
// It names the Secret handle, item, or variable in a failure, never a value --
// there is no value here to leak, and the message is for whoever is editing.
func (s *Service) validateConsumption(ctx context.Context, spaceID string, c agentdef.SecretConsumption) error {
	if c.IsEmpty() {
		return nil
	}
	if s.Secrets == nil {
		return ErrSecretsNotConfigured
	}
	// The resolved variable names of a whole revision must not collide.
	resolved := map[string]struct{}{}
	for _, g := range c.Env {
		if g.Secret == "" {
			return apierr.New(apierr.KindInvalid, "secret consumption: a grant names no secret")
		}
		sec, err := s.Secrets.GetSecret(ctx, g.Secret)
		if err != nil {
			return err
		}
		// Not-found rather than forbidden for a missing or another space's
		// Secret: the answer must not confirm that a Secret exists elsewhere.
		if sec == nil || sec.SpaceID != spaceID {
			return notInSpace(g.Secret)
		}
		if sec.State == coresecret.StateDestroyed {
			return apierr.New(apierr.KindInvalid, "secret consumption: secret "+g.Secret+" is destroyed")
		}
		names, err := resolveGrantEnvNames(g, sec)
		if err != nil {
			return err
		}
		for _, n := range names {
			if !agentdef.IsEnvName(n) {
				return apierr.New(apierr.KindInvalid, "secret consumption: "+n+" is not a valid environment variable name")
			}
			if agentdef.IsReservedEnvName(n) {
				return apierr.New(apierr.KindInvalid, "secret consumption: environment variable "+n+" is reserved for the platform ("+agentdef.ReservedEnvPrefix+"*) and cannot be a Secret grant target")
			}
			if _, dup := resolved[n]; dup {
				return apierr.New(apierr.KindInvalid, "secret consumption: environment variable "+n+" is set by more than one grant")
			}
			resolved[n] = struct{}{}
		}
	}
	return nil
}

func notInSpace(secretID string) error {
	return apierr.New(apierr.KindInvalid, "secret consumption: secret "+secretID+" is not in this space")
}

// resolveGrantEnvNames returns the environment variable names one grant sets,
// and rejects a malformed grant: a selected item needs env_name and no prefix,
// a whole group needs no env_name and takes each item under its own name.
func resolveGrantEnvNames(g agentdef.SecretEnvGrant, sec *coresecret.Secret) ([]string, error) {
	if g.WholeGroup() {
		if g.EnvName != "" {
			return nil, apierr.New(apierr.KindInvalid, "secret consumption: a whole-group grant on secret "+g.Secret+" must not set env_name")
		}
		out := make([]string, 0, len(sec.ItemNames))
		for _, item := range sec.ItemNames {
			out = append(out, g.Prefix+item)
		}
		return out, nil
	}
	if g.EnvName == "" {
		return nil, apierr.New(apierr.KindInvalid, "secret consumption: item "+g.Item+" of secret "+g.Secret+" needs an env_name")
	}
	if g.Prefix != "" {
		return nil, apierr.New(apierr.KindInvalid, "secret consumption: prefix applies to a whole group, not the selected item "+g.Item)
	}
	if !slices.Contains(sec.ItemNames, g.Item) {
		return nil, apierr.New(apierr.KindInvalid, "secret consumption: secret "+g.Secret+" has no item "+g.Item)
	}
	return []string{g.EnvName}, nil
}

// GrantProblem is one Secret grant of an Agent that a run started now would
// not be given. It names the grant only by what the Agent's own consumption
// already shows -- the Secret's ID and item -- plus the problem, and nothing
// else about the Secret, so every member who can read and run the Agent may
// read it. See docs/design/space-secrets.md §10.
type GrantProblem struct {
	SecretID string
	Item     string
	Problem  coretask.SecretProblem
	// Optional is a grant a run skips rather than fails on.
	Optional bool
}

// SecretGrantProblems reports, per agent ID, the grants that would not resolve
// now, by the rules the worker route applies when it materializes them, read
// from metadata rather than values. A Secret that cannot be read is left out:
// the answer is advisory, and the run itself still decides.
func (s *Service) SecretGrantProblems(ctx context.Context, agents []agentdef.Agent) map[string][]GrantProblem {
	if s.Secrets == nil {
		return nil
	}
	out := map[string][]GrantProblem{}
	read := map[string]*coresecret.Secret{}
	failed := map[string]bool{}
	for _, a := range agents {
		for _, g := range a.SecretConsumption.Env {
			if g.Secret == "" || failed[g.Secret] {
				continue
			}
			sec, ok := read[g.Secret]
			if !ok {
				got, err := s.Secrets.GetSecret(ctx, g.Secret)
				if err != nil && !errors.Is(err, apierr.ErrNotFound) {
					slog.Warn("agent service: secret unreadable for grant health", "agent_id", a.ID, "secret_id", g.Secret, "err", err)
					failed[g.Secret] = true
					continue
				}
				sec = got
				read[g.Secret] = sec
			}
			if problem := grantProblem(g, sec, a.SpaceID); problem != "" {
				out[a.ID] = append(out[a.ID], GrantProblem{SecretID: g.Secret, Item: g.Item, Problem: problem, Optional: g.Optional})
			}
		}
	}
	return out
}

// grantProblem is why one grant would not resolve, or "" when it would. Another
// Space's Secret reads as unavailable, the same as a missing one, so the answer
// confirms nothing outside the Agent's Space.
func grantProblem(g agentdef.SecretEnvGrant, sec *coresecret.Secret, spaceID string) coretask.SecretProblem {
	switch {
	case sec == nil || sec.SpaceID != spaceID || sec.State == coresecret.StateDestroyed:
		return coretask.SecretUnavailable
	case sec.State != coresecret.StateActive:
		return coretask.SecretDisabled
	case !g.WholeGroup() && !slices.Contains(sec.ItemNames, g.Item):
		return coretask.SecretItemMissing
	default:
		return ""
	}
}
