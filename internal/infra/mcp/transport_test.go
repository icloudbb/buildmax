package mcp

import (
	"os"
	"strings"
	"testing"

	coremcp "github.com/icloudbb/buildmax/internal/core/mcp"
)

func TestMergedEnv(t *testing.T) {
	base := os.Environ()
	merged := mergedEnv(map[string]string{"BUILDMAX_MCP_TEST_ONLY": "1"})
	if merged == nil {
		t.Fatal("nil")
	}
	var has bool
	for _, e := range merged {
		if strings.HasPrefix(e, "BUILDMAX_MCP_TEST_ONLY=1") {
			has = true
		}
	}
	if !has {
		t.Fatalf("missing override in %v", merged)
	}
	// Original keys should still be present (approximate length).
	if len(merged) < len(base) {
		t.Fatalf("merged shorter than base: %d vs %d", len(merged), len(base))
	}
}

// The entries Windows puts in a process environment cannot be created with
// os.Setenv on any platform — a name containing '=' is rejected — so the merge
// is given them directly. Dropping them made merged shorter than base, which is
// how the Windows CI job found this.
func TestMergeEnvKeepsWindowsDriveEntries(t *testing.T) {
	base := []string{
		"=C:=C:\\Users\\runneradmin",
		"=ExitCode=00000000",
		"PATH=/usr/bin",
		"REPLACED=old",
		"malformed-entry-without-a-separator",
	}
	merged := mergeEnv(base, map[string]string{"REPLACED": "new"})

	want := map[string]bool{
		"=C:=C:\\Users\\runneradmin": true,
		"=ExitCode=00000000":         true,
		"PATH=/usr/bin":              true,
		"REPLACED=new":               true,
	}
	for _, e := range merged {
		if !want[e] {
			t.Errorf("merged holds unexpected entry %q", e)
			continue
		}
		delete(want, e)
	}
	for e := range want {
		t.Errorf("merged dropped %q", e)
	}
}

func TestMergedEnv_nil(t *testing.T) {
	if mergedEnv(nil) != nil {
		t.Fatal("want nil")
	}
	if mergedEnv(map[string]string{}) != nil {
		t.Fatal("want nil")
	}
}

func TestBearerTokenRefusesCleartextRemoteEndpoint(t *testing.T) {
	t.Setenv("TEST_MCP_SECRET", "secret")
	_, err := newTransport(coremcp.ServerConfig{
		Type: coremcp.TransportHTTP, URL: "http://example.com/mcp", BearerTokenEnv: "TEST_MCP_SECRET",
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("cleartext endpoint accepted: %v", err)
	}
}
