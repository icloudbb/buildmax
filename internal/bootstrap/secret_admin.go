package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/db"
	infrasecret "github.com/icloudbb/buildmax/internal/infra/secret"
)

// The operator side of KEK rotation. The key file is the operator's: adding a
// key, moving `current`, and removing a retired key are edits to it. This
// command does the one step the operator cannot do by hand, moving every stored
// row onto the current key, and reports which keys rows still depend on. See
// docs/design/space-secrets.md §9.1.

// SecretCommandUsage is the help text for `buildmax-server secret`.
const SecretCommandUsage = `Usage: buildmax-server secret <command>

Commands:
  rewrap
        Re-wrap every stored data key (Space Secrets and managed-model
        credentials) under the KEK file's current key, then report how many
        rows each key still protects. Values are not decrypted or re-encrypted.
        Safe to run on a live deployment and to re-run; an interrupted run
        resumes where it stopped.

A key may be removed from the KEK file once rewrap reports no row under it.
The server refuses to start while a stored row names a key the file does not
hold.
`

// RunSecretCommand executes `buildmax-server secret ...`. args excludes the
// "secret" word itself.
func RunSecretCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, SecretCommandUsage)
		return errors.New("secret: a command is required")
	}
	switch args[0] {
	case "rewrap":
		if len(args) > 1 {
			fmt.Fprint(out, SecretCommandUsage)
			return fmt.Errorf("rewrap: unexpected argument %q", args[1])
		}
		return runSecretRewrap(ctx, out)
	case "help", "-h", "--help":
		fmt.Fprint(out, SecretCommandUsage)
		return nil
	default:
		fmt.Fprint(out, SecretCommandUsage)
		return fmt.Errorf("secret: unknown command %q", args[0])
	}
}

func runSecretRewrap(ctx context.Context, out io.Writer) error {
	sc, err := config.LoadServerConfig()
	if err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	if sc.Secret.KEKFile == "" {
		return fmt.Errorf("secret.kek_file is not configured in %s; there is no key to rewrap under",
			config.ServerConfigPath())
	}
	store, err := openStore(ctx, sc.Database)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	// The same load the server does, so a key file missing a referenced key
	// fails here, before any row is touched, with the same message.
	kek, err := loadDeploymentKEK(ctx, sc.Secret.KEKFile, store)
	if err != nil {
		return err
	}
	return rewrapSealedRows(ctx, store, kek, out)
}

// rewrapSealedRows moves the store's sealed rows to the current KEK and prints
// what moved and what each loaded key still protects.
func rewrapSealedRows(ctx context.Context, store *db.Store, kek infrasecret.KEKProvider, out io.Writer) error {
	current := kek.CurrentKeyID()
	res, rewrapErr := store.RewrapSealedKeys(ctx, infrasecret.NewCipher(kek))

	var moved int64
	for _, id := range sortedKeys(res.Rewrapped) {
		fmt.Fprintf(out, "rewrapped %s from %s to %s\n", rowCount(res.Rewrapped[id]), id, current)
		moved += res.Rewrapped[id]
	}
	if moved == 0 && rewrapErr == nil {
		fmt.Fprintf(out, "every row was already under %s\n", current)
	}
	if res.Skipped > 0 {
		fmt.Fprintf(out, "left %s a concurrent edit had already rewritten under %s\n", rowCount(res.Skipped), current)
	}
	// KEK rotation changes no Space value, so it is an operational log line
	// rather than a Space audit event (space-secrets.md §9.1).
	slog.InfoContext(ctx, "kek rewrap", "current_key_id", current, "rewrapped", moved,
		"skipped", res.Skipped, "failed", rewrapErr != nil)
	if rewrapErr != nil {
		return fmt.Errorf("rewrap stopped: %w; rows already moved stay moved, and re-running resumes", rewrapErr)
	}

	refs, err := store.SealedKeyReferences(ctx, infrasecret.SealedValueKeyID)
	if err != nil {
		return fmt.Errorf("count sealed rows by key: %w", err)
	}
	fmt.Fprintln(out, "\nrows by key:")
	for _, id := range kek.KeyIDs() {
		switch {
		case id == current:
			fmt.Fprintf(out, "  %s  %d (current)\n", id, refs[id])
		case refs[id] == 0:
			fmt.Fprintf(out, "  %s  0 (no row uses it; it can be removed from the key file)\n", id)
		default:
			// A row written by a server that had not yet loaded the new
			// current key lands here; running rewrap again moves it.
			fmt.Fprintf(out, "  %s  %d (still in use; run rewrap again after every server uses the new current key)\n", id, refs[id])
		}
	}
	return nil
}

// loadDeploymentKEK loads the deployment KEK and refuses one that cannot open
// every stored row. It returns nil when no key file is configured and nothing is
// sealed. This is where removing a key that rows still name is refused: at the
// next load of the file, before a server serves or a rewrap writes.
func loadDeploymentKEK(ctx context.Context, kekFile string, store *db.Store) (infrasecret.KEKProvider, error) {
	var kek infrasecret.KEKProvider
	if kekFile != "" {
		loaded, err := infrasecret.LoadKEKFile(kekFile)
		if err != nil {
			return nil, err
		}
		kek = loaded
	}
	refs, err := store.SealedKeyReferences(ctx, infrasecret.SealedValueKeyID)
	if err != nil {
		return nil, fmt.Errorf("count sealed rows by key: %w", err)
	}
	if err := infrasecret.RequireLoadedKeys(kek, refs); err != nil {
		return nil, err
	}
	return kek, nil
}

func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func rowCount(n int64) string {
	if n == 1 {
		return "1 row"
	}
	return fmt.Sprintf("%d rows", n)
}
