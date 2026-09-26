package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/service/storagecheck"
)

// storageRecords is a one-page database: every list returns its rows on the
// first call and nothing after, which is all the command's wiring needs.
type storageRecords struct {
	artifacts   []coreartifact.Artifact
	checkpoints []coretask.CheckpointPayloadRef
	traces      []coretask.RunTraceRef
	releases    []coreplugin.Release
}

func onlyFirst[T any](after string, rows []T) []T {
	if after != "" {
		return nil
	}
	return rows
}

func (r *storageRecords) ListLiveArtifactsAfter(_ context.Context, after string, _ int) ([]coreartifact.Artifact, error) {
	return onlyFirst(after, r.artifacts), nil
}

func (r *storageRecords) ListWorkspaceCheckpointPayloadsAfter(_ context.Context, after string, _ int) ([]coretask.CheckpointPayloadRef, error) {
	return onlyFirst(after, r.checkpoints), nil
}

func (r *storageRecords) ListTaskRunTracesAfter(_ context.Context, after string, _ int) ([]coretask.RunTraceRef, error) {
	return onlyFirst(after, r.traces), nil
}

func (r *storageRecords) ListPluginReleasesAfter(_ context.Context, afterName, _ string, _ int) ([]coreplugin.Release, error) {
	return onlyFirst(afterName, r.releases), nil
}

func sum(b string) string {
	s := sha256.Sum256([]byte(b))
	return hex.EncodeToString(s[:])
}

// TestStorageVerifyAgainstTheLocalBackend drives the command's report over the
// real local_fs stores the server builds, so the adapters — artifact directory,
// checkpoint tree, package directory, and the trace's on-disk fallback — are
// the ones a deployment resolves through.
func TestStorageVerifyAgainstTheLocalBackend(t *testing.T) {
	ctx := t.Context()
	dir := t.TempDir()
	storage, err := buildBlobStorage(ctx, config.ServerStorageConfig{}, dir)
	if err != nil {
		t.Fatalf("buildBlobStorage: %v", err)
	}

	if _, err := storage.artifact.PutArtifact(ctx, coreartifact.Ref{SpaceID: "s1", ArtifactID: "a1"}, strings.NewReader("report")); err != nil {
		t.Fatalf("PutArtifact: %v", err)
	}
	ckptKey, err := storage.checkpoint.Put(ctx, "s1", sum("tar"), strings.NewReader("tar"))
	if err != nil {
		t.Fatalf("checkpoint Put: %v", err)
	}
	pkgKey, err := storage.packages.PackageKey(storage.packageKeyPrefix, "review", "sha256:"+sum("pkg"))
	if err != nil {
		t.Fatalf("PackageKey: %v", err)
	}
	if err := storage.packages.Put(ctx, pkgKey, strings.NewReader("pkg-altered")); err != nil {
		t.Fatalf("package Put: %v", err)
	}
	traceDir := config.RunGlobalDir(dir, "s1", "t1", "r1")
	if err := os.MkdirAll(filepath.Join(traceDir, "traces"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(traceDir, "traces", "r1.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	records := &storageRecords{
		artifacts: []coreartifact.Artifact{
			{ID: "a1", SpaceID: "s1", SHA256: sum("report"), SizeBytes: 6},
			{ID: "a2", SpaceID: "s1", SHA256: sum("lost"), SizeBytes: 4},
		},
		checkpoints: []coretask.CheckpointPayloadRef{
			{CheckpointID: "c1", SpaceID: "s1", StorageKey: ckptKey, PayloadSHA256: sum("tar"), SizeBytes: 3},
		},
		traces: []coretask.RunTraceRef{
			{SpaceID: "s1", TaskID: "t1", TaskRunID: "r1", TracePath: "traces/r1.jsonl"},
			{SpaceID: "s1", TaskID: "t1", TaskRunID: "r2", TracePath: "traces/r2.jsonl"},
		},
		releases: []coreplugin.Release{
			{PluginName: "review", Version: "1.0.0", Digest: "sha256:" + sum("pkg"), ObjectKey: pkgKey, SizeBytes: 3},
		},
	}
	newChecker := func(checksums bool) *storagecheck.Checker {
		return &storagecheck.Checker{
			Records:     records,
			Artifacts:   storage.artifact,
			Checkpoints: storage.checkpoint,
			Traces:      traceObjects{runs: storage.persist, workspacesDir: dir},
			Packages:    storage.packages,
			Checksums:   checksums,
		}
	}

	var out strings.Builder
	err = verifyStorage(ctx, newChecker(false), &out)
	if err == nil || !strings.Contains(err.Error(), "2 stored references do not resolve") {
		t.Fatalf("existence check err = %v, want the two missing objects\n%s", err, out.String())
	}
	for _, line := range []string{
		"missing    artifact a2 (space s1): no object at the recorded location",
		"missing    trace r2 (space s1): no object at the recorded location",
	} {
		if !strings.Contains(out.String(), line) {
			t.Errorf("output lacks %q\n%s", line, out.String())
		}
	}
	if strings.Contains(out.String(), "plugin_release review") {
		t.Errorf("existence mode read the package's content\n%s", out.String())
	}

	out.Reset()
	err = verifyStorage(ctx, newChecker(true), &out)
	if err == nil || !strings.Contains(err.Error(), "3 stored references do not resolve") {
		t.Fatalf("checksum check err = %v, want the altered package as well\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "mismatch   plugin_release review@1.0.0: object is 11 bytes, record says 3") {
		t.Errorf("output lacks the package mismatch\n%s", out.String())
	}

	// With the missing rows gone and the package repaired, the same check is clean.
	if err := storage.packages.Put(ctx, pkgKey, strings.NewReader("pkg")); err != nil {
		t.Fatalf("package Put: %v", err)
	}
	records.artifacts = records.artifacts[:1]
	records.traces = records.traces[:1]
	out.Reset()
	if err := verifyStorage(ctx, newChecker(true), &out); err != nil {
		t.Fatalf("clean store: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Every stored reference resolves.") {
		t.Errorf("clean output lacks the verdict\n%s", out.String())
	}
}

func TestStorageCommandRejectsUnknownSubcommand(t *testing.T) {
	var out strings.Builder
	if err := RunStorageCommand(t.Context(), []string{"repair"}, &out); err == nil {
		t.Fatal("an unknown subcommand must fail")
	}
	if !strings.Contains(out.String(), "buildmax-server storage <command>") {
		t.Errorf("usage not printed:\n%s", out.String())
	}
}
