package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	"github.com/icloudbb/buildmax/internal/service/storagecheck"
)

// The operator-side proof that a restored database and bucket agree
// (verification-program.md V19). It runs here, beside the server, because it
// needs the same database and the same storage configuration the server reads,
// and because after a restore the server may be the thing still in doubt.

// StorageCommandUsage is the help text for `buildmax-server storage`.
const StorageCommandUsage = `Usage: buildmax-server storage <command> [flags]

Commands:
  verify [--checksums]
        Check that every stored object the database refers to is there: live
        artifacts, workspace checkpoint payloads, run traces, and plugin release
        packages. Prints each reference that is missing, altered, or unreadable
        with its id, then a count per kind, and exits non-zero if it found any.
        Read-only: it repairs and deletes nothing.

        --checksums   Also read every object and compare its size and SHA-256
                      with the record. This reads every stored byte; without
                      it the check only proves each object exists.
`

// storageProgressEvery is how many references pass between progress lines, so
// a long walk shows it is moving without printing a line per page.
const storageProgressEvery = 1000

// RunStorageCommand executes `buildmax-server storage ...`. args excludes the
// "storage" word itself.
func RunStorageCommand(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(out, StorageCommandUsage)
		return errors.New("storage: a command is required")
	}
	switch args[0] {
	case "verify":
		return runStorageVerify(ctx, args[1:], out)
	case "help", "-h", "--help":
		fmt.Fprint(out, StorageCommandUsage)
		return nil
	default:
		fmt.Fprint(out, StorageCommandUsage)
		return fmt.Errorf("storage: unknown command %q", args[0])
	}
}

func runStorageVerify(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("storage verify", flag.ContinueOnError)
	fs.SetOutput(out)
	checksums := fs.Bool("checksums", false, "read every object and compare its size and SHA-256")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("storage verify: unexpected argument %q", fs.Arg(0))
	}

	sc, err := config.LoadServerConfig()
	if err != nil {
		return fmt.Errorf("server config: %w", err)
	}
	workspacesDir, err := resolveWorkspacesDir(sc.WorkspacesDir)
	if err != nil {
		return err
	}
	store, err := openStore(ctx, sc.Database)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	storage, err := buildBlobStorage(ctx, sc.Storage, workspacesDir)
	if err != nil {
		return err
	}

	checker := &storagecheck.Checker{
		Records:     store,
		Artifacts:   storage.artifact,
		Checkpoints: storage.checkpoint,
		Traces:      traceObjects{runs: storage.persist, workspacesDir: workspacesDir},
		Packages:    storage.packages,
		Checksums:   *checksums,
	}
	return verifyStorage(ctx, checker, out)
}

// verifyStorage runs the check and writes the operator's report: each finding
// as it is found, progress on long walks, and a closing count per kind.
func verifyStorage(ctx context.Context, checker *storagecheck.Checker, out io.Writer) error {
	mode := "existence only; add --checksums to also compare size and SHA-256"
	if checker.Checksums {
		mode = "existence, size, and SHA-256"
	}
	fmt.Fprintf(out, "Verifying stored references (%s).\n", mode)
	fmt.Fprintln(out, "Tombstoned artifacts and traces cleared by retention are not references and are skipped.")

	lastReported := map[storagecheck.Kind]int{}
	checker.OnFinding = func(f storagecheck.Finding) {
		where := ""
		if f.SpaceID != "" {
			where = " (space " + f.SpaceID + ")"
		}
		fmt.Fprintf(out, "%-10s %s %s%s: %s\n", f.Problem, f.Kind, f.ID, where, f.Detail)
	}
	checker.OnProgress = func(kind storagecheck.Kind, c storagecheck.Counts, done bool) {
		if done || c.Checked-lastReported[kind] < storageProgressEvery {
			return
		}
		lastReported[kind] = c.Checked
		fmt.Fprintf(out, "... %s: %d checked, %d findings so far\n", kind, c.Checked, c.Findings())
	}

	report, err := checker.Run(ctx)
	fmt.Fprintf(out, "\n%-22s %9s %9s %11s %11s\n", "KIND", "CHECKED", "MISSING", "MISMATCHED", "UNREADABLE")
	for _, kind := range storagecheck.Kinds() {
		c, ok := report[kind]
		if !ok {
			continue
		}
		fmt.Fprintf(out, "%-22s %9d %9d %11d %11d\n", kind, c.Checked, c.Missing, c.Mismatched, c.Unreadable)
	}
	if err != nil {
		// The counts above cover only what was walked before the database
		// stopped answering, so they are not a verdict either way.
		return fmt.Errorf("storage verify incomplete: %w", err)
	}
	if n := report.Findings(); n > 0 {
		return fmt.Errorf("%d stored references do not resolve", n)
	}
	fmt.Fprintln(out, "\nEvery stored reference resolves.")
	return nil
}

// traceObjects resolves a run's trace the way the trace route does: run-global
// object storage first, then the run's directory on this server's disk, which
// is where a local_fs deployment keeps it. A trace is read whole, one at a time;
// traces are bounded by the recorder.
type traceObjects struct {
	runs          blob.RunStorage
	workspacesDir string
}

func (t traceObjects) OpenTrace(ctx context.Context, ref coretask.RunTraceRef) (io.ReadCloser, error) {
	clean, err := blob.CleanRelPath(ref.TracePath)
	if err != nil {
		return nil, fmt.Errorf("recorded trace path %q: %w", ref.TracePath, err)
	}
	data, err := t.runs.GetRunGlobal(ctx, blob.RunObjectRef{
		SpaceID: ref.SpaceID, TaskID: ref.TaskID, TaskRunID: ref.TaskRunID, RelPath: clean,
	})
	if err == nil {
		return io.NopCloser(bytes.NewReader(data)), nil
	}
	if !errors.Is(err, apierr.ErrNotFound) && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return os.Open(filepath.Join(config.RunGlobalDir(t.workspacesDir, ref.SpaceID, ref.TaskID, ref.TaskRunID), filepath.FromSlash(clean)))
}
