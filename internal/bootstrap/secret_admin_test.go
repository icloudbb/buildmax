package bootstrap

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
)

func TestSecretCommandRefusesWhatItDoesNotRun(t *testing.T) {
	ctx := context.Background()
	for _, args := range [][]string{nil, {"rotate"}, {"rewrap", "--all"}} {
		var out bytes.Buffer
		if err := RunSecretCommand(ctx, args, &out); err == nil {
			t.Errorf("secret %v: want an error", args)
		}
		if !strings.Contains(out.String(), "rewrap") {
			t.Errorf("secret %v: usage not printed:\n%s", args, out.String())
		}
	}
}

// A rewrap with no key file has nothing to wrap under; it says which setting is
// missing rather than opening the database.
func TestSecretRewrapNeedsAKeyFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvKeyBuildmaxHome, home)
	if err := os.WriteFile(filepath.Join(home, "server.yaml"), []byte("port: 5678\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := RunSecretCommand(context.Background(), []string{"rewrap"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "secret.kek_file is not configured") {
		t.Fatalf("err = %v, want the missing setting named", err)
	}
}
