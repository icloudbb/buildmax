package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The drill replaces every credential on its cluster, so it must never reach
// one this worktree did not create for itself.
func TestRotationDrillRunsOnlyOnThisWorktreesEphemeralCluster(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("BUILDMAX_KIND_CLUSTER", "")

	err := requireEphemeralKindCluster()
	if err == nil || !strings.Contains(err.Error(), "BUILDMAX_KIND_EPHEMERAL=1") {
		t.Fatalf("with no ephemeral record: err = %v, want a refusal naming BUILDMAX_KIND_EPHEMERAL=1", err)
	}

	if err := writeEphemeralKind(ephemeralKind{cluster: "buildmax-eph-abcd1234", portalPort: "18080", tlsPort: "18443"}); err != nil {
		t.Fatal(err)
	}
	if err := requireEphemeralKindCluster(); err != nil {
		t.Fatalf("on the recorded ephemeral cluster: %v", err)
	}

	t.Setenv("BUILDMAX_KIND_CLUSTER", defaultKindCluster)
	if err := requireEphemeralKindCluster(); err == nil {
		t.Fatal("an explicit BUILDMAX_KIND_CLUSTER naming the resident cluster was not refused")
	}
}

func TestKindDrillNamesItsDrills(t *testing.T) {
	for _, args := range [][]string{nil, {"smoke"}, {"rotation", "extra"}} {
		if err := cmdKindDrill(args); err == nil {
			t.Errorf("cmdKindDrill(%q) accepted an unknown drill", args)
		}
	}
}

// The forged token must carry the original claims and verify under exactly the
// secret it was signed with, or a 401 would prove nothing about the old key.
func TestReSignAccessTokenKeepsClaimsUnderTheGivenSecret(t *testing.T) {
	claims, _ := json.Marshal(map[string]any{"sub": "user-1", "sid": "session-1", "typ": "access", "exp": 1})
	original := "eyJhbGciOiJIUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(claims) + ".sig"

	token, err := reSignAccessToken(original, "old-secret")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	mac := hmac.New(sha256.New, []byte("old-secret"))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if parts[2] != base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("the token is not signed with the given secret")
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatal(err)
	}
	if got["sub"] != "user-1" || got["sid"] != "session-1" || got["typ"] != "access" {
		t.Fatalf("claims = %v, want the original subject, session, and type", got)
	}
	if exp, _ := got["exp"].(float64); int64(exp) <= time.Now().Unix() {
		t.Fatalf("exp = %v, want a time in the future", got["exp"])
	}
}

func TestKindMCImageIsThePinnedInitImage(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	image, err := kindMCImage()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("deployment", "kind", "minio-init.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(image, "@sha256:") || !strings.Contains(string(data), image) {
		t.Fatalf("image = %q, want the digest-pinned image minio-init.yaml runs", image)
	}
}
