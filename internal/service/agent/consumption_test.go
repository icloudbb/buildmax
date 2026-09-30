package agent

import (
	"context"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
)

// fakeSecrets answers GetSecret from a fixed set keyed by public id.
type fakeSecrets map[string]*coresecret.Secret

func (f fakeSecrets) GetSecret(_ context.Context, id string) (*coresecret.Secret, error) {
	if s, ok := f[id]; ok {
		return s, nil
	}
	return nil, nil
}

func env(grants ...agentdef.SecretEnvGrant) agentdef.SecretConsumption {
	return agentdef.SecretConsumption{Env: grants}
}

func TestValidateConsumption(t *testing.T) {
	const space = "tm_1"
	secrets := fakeSecrets{
		"sec_aws":   {ID: "sec_aws", SpaceID: space, State: coresecret.StateActive, ItemNames: []string{"access_key_id", "secret_access_key", "region"}},
		"sec_gh":    {ID: "sec_gh", SpaceID: space, State: coresecret.StateActive, ItemNames: []string{"token"}},
		"sec_dead":  {ID: "sec_dead", SpaceID: space, State: coresecret.StateDestroyed, ItemNames: nil},
		"sec_other": {ID: "sec_other", SpaceID: "tm_2", State: coresecret.StateActive, ItemNames: []string{"token"}},
	}
	svc := &Service{Secrets: secrets}

	cases := []struct {
		name    string
		cons    agentdef.SecretConsumption
		wantErr bool
	}{
		{"empty is fine", agentdef.SecretConsumption{}, false},
		{"selected item ok", env(agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"}), false},
		{"whole group ok", env(agentdef.SecretEnvGrant{Secret: "sec_aws", Prefix: "AWS_"}), false},
		{"two secrets side by side", env(
			agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"},
			agentdef.SecretEnvGrant{Secret: "sec_aws", Item: "region", EnvName: "AWS_REGION"},
		), false},

		{"unknown secret", env(agentdef.SecretEnvGrant{Secret: "sec_nope", Item: "x", EnvName: "X"}), true},
		{"another space's secret", env(agentdef.SecretEnvGrant{Secret: "sec_other", Item: "token", EnvName: "T"}), true},
		{"destroyed secret", env(agentdef.SecretEnvGrant{Secret: "sec_dead", Item: "token", EnvName: "T"}), true},
		{"item not in secret", env(agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "missing", EnvName: "X"}), true},
		{"selected item needs env_name", env(agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token"}), true},
		{"whole group must not set env_name", env(agentdef.SecretEnvGrant{Secret: "sec_gh", EnvName: "X"}), true},
		{"prefix only on whole group", env(agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "T", Prefix: "P_"}), true},
		{"invalid env name", env(agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "GH-TOKEN"}), true},
		{"no secret named", env(agentdef.SecretEnvGrant{Item: "token", EnvName: "T"}), true},

		// A grant may not deliver an item under a platform variable name: the
		// worker sets those itself, and letting a Space's config name one would
		// let it shadow a variable the runtime relies on.
		{"reserved env name refused", env(agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "BUILDMAX_RUN_TOKEN"}), true},
		{"reserved prefix on a whole group refused", env(agentdef.SecretEnvGrant{Secret: "sec_aws", Prefix: "BUILDMAX_"}), true},

		{"named collision", env(
			agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "DUP"},
			agentdef.SecretEnvGrant{Secret: "sec_aws", Item: "region", EnvName: "DUP"},
		), true},
		{"whole-group vs named collision", env(
			agentdef.SecretEnvGrant{Secret: "sec_aws"},                                  // yields access_key_id, secret_access_key, region
			agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "region"}, // collides with sec_aws's region
		), true},
		{"prefix resolves a collision", env(
			agentdef.SecretEnvGrant{Secret: "sec_aws", Prefix: "AWS_"},
			agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "region"},
		), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := svc.validateConsumption(context.Background(), space, tc.cons)
			if tc.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err != nil {
				if kind, _ := apierr.KindOf(err); kind != apierr.KindInvalid && kind != apierr.KindNotConfigured {
					t.Fatalf("error kind = %q, want invalid", kind)
				}
			}
		})
	}
}

func TestValidateConsumption_NoStoreRefusesNonEmpty(t *testing.T) {
	svc := &Service{} // Secrets nil
	if err := svc.validateConsumption(context.Background(), "tm", agentdef.SecretConsumption{}); err != nil {
		t.Fatalf("empty consumption must not need a store: %v", err)
	}
	err := svc.validateConsumption(context.Background(), "tm",
		env(agentdef.SecretEnvGrant{Secret: "s", Item: "i", EnvName: "X"}))
	if err == nil {
		t.Fatal("consuming a secret with no store configured must be refused")
	}
}
