package config

import (
	"strings"
	"testing"
)

func TestEnvVarsHaveUniqueNames(t *testing.T) {
	seen := map[string]struct{}{}
	for _, envVar := range EnvVars() {
		if _, ok := seen[envVar.Name]; ok {
			t.Fatalf("EnvVars contains duplicate env name %q", envVar.Name)
		}
		seen[envVar.Name] = struct{}{}
	}
}

func TestEnvVarsIncludeRequiredKeys(t *testing.T) {
	required := []string{EnvKeyBuildmaxHome, EnvKeyBuildmaxServerURL, EnvKeyBuildmaxJWTSecret}
	names := map[string]struct{}{}
	for _, e := range EnvVars() {
		names[e.Name] = struct{}{}
	}
	for _, k := range required {
		if _, ok := names[k]; !ok {
			t.Errorf("EnvVars missing required key %q", k)
		}
	}
}

func TestEnvVarsReturnsCopy(t *testing.T) {
	vars := EnvVars()
	vars[0].Name = "BROKEN"
	if got := EnvVars()[0].Name; got != EnvKeyBuildmaxHome {
		t.Fatalf("environment specification mutated through caller slice: %q", got)
	}
}

// TestCredentialNamedVariablesAreMarked keeps a new secret from reaching a
// worker Job as a plain value: one named like a credential but left unmarked
// would be copied into the Job spec instead of referenced from the Secret.
func TestCredentialNamedVariablesAreMarked(t *testing.T) {
	for _, v := range EnvVars() {
		named := false
		for _, part := range []string{"_KEY", "_SECRET", "_TOKEN", "_PASSWORD"} {
			if strings.HasSuffix(v.Name, part) {
				named = true
			}
		}
		if named && !v.Credential {
			t.Errorf("%s is named like a credential but not marked Credential", v.Name)
		}
	}
}
