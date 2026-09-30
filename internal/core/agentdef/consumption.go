package agentdef

import (
	"regexp"
	"sort"
	"strings"
)

// SecretConsumption is how an Agent revision consumes Space Secrets. It is part
// of the Agent definition and versioned with it, so a run's grants come from
// immutable state while the Secret values behind them stay live. Env is the
// only delivery today; file rendering (docs/design/space-secrets.md §6.3) is
// added with its renderers. See §6.
type SecretConsumption struct {
	Env []SecretEnvGrant `json:"env,omitempty"`
}

// SecretEnvGrant delivers a Space Secret into a run's environment in one of two
// forms. A selected item sets Item and EnvName: that item arrives under that
// variable name. The whole group leaves Item empty: every item arrives under
// its own name, optionally with Prefix. Optional inverts the default: a grant
// is required unless it says otherwise, and a required grant that cannot be
// produced fails the run before the Agent starts.
type SecretEnvGrant struct {
	// Secret is the public id of a Secret in the Agent's own Space.
	Secret string `json:"secret"`
	// Item names one item of the group; empty means the whole group.
	Item string `json:"item,omitempty"`
	// EnvName is the variable name for a selected item. Unused for a whole
	// group, which uses each item's own name.
	EnvName string `json:"env_name,omitempty"`
	// Prefix is prepended to each item name when the whole group is taken.
	Prefix string `json:"prefix,omitempty"`
	// Optional makes a missing grant a skip rather than a run failure.
	Optional bool `json:"optional,omitempty"`
}

// WholeGroup reports whether this grant takes every item of its Secret.
func (g SecretEnvGrant) WholeGroup() bool { return g.Item == "" }

// envNamePattern is the identifier a variable name must be, so a whole group
// can be injected without an item that cannot become a variable.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// IsEnvName reports whether s is a valid environment variable name.
func IsEnvName(s string) bool { return envNamePattern.MatchString(s) }

// ReservedEnvPrefix is the namespace the platform sets in a worker's own
// environment (BUILDMAX_RUN_TOKEN, BUILDMAX_HOME, the storage keys, …). A
// Secret grant may not deliver an item under a name in it: doing so would let a
// Space's own configuration name a variable the runtime relies on. See
// internal/config/env_spec.go for the variables themselves.
const ReservedEnvPrefix = "BUILDMAX_"

// IsReservedEnvName reports whether name belongs to the platform's reserved
// environment namespace, so a Secret grant that would set it can be refused.
func IsReservedEnvName(name string) bool {
	return strings.HasPrefix(name, ReservedEnvPrefix)
}

// Canonical returns the consumption in a stable order, so reordering grants is
// not an edit that appends a revision. It does not validate; that is the
// service's job against live Space Secrets.
func (c SecretConsumption) Canonical() SecretConsumption {
	if len(c.Env) == 0 {
		return SecretConsumption{}
	}
	env := make([]SecretEnvGrant, len(c.Env))
	copy(env, c.Env)
	sort.Slice(env, func(i, j int) bool {
		a, b := env[i], env[j]
		if a.Secret != b.Secret {
			return a.Secret < b.Secret
		}
		if a.Item != b.Item {
			return a.Item < b.Item
		}
		return a.EnvName < b.EnvName
	})
	return SecretConsumption{Env: env}
}

// Equal reports whether two consumptions are the same up to grant order.
func (c SecretConsumption) Equal(other SecretConsumption) bool {
	ca, cb := c.Canonical().Env, other.Canonical().Env
	if len(ca) != len(cb) {
		return false
	}
	for i := range ca {
		if ca[i] != cb[i] {
			return false
		}
	}
	return true
}

// IsEmpty reports whether the consumption declares nothing.
func (c SecretConsumption) IsEmpty() bool { return len(c.Env) == 0 }
