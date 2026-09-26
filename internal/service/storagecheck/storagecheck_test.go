package storagecheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path"
	"sort"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

func digest(b string) string {
	sum := sha256.Sum256([]byte(b))
	return hex.EncodeToString(sum[:])
}

// fakeRecords pages each kind by its handle, the contract the MySQL store
// keeps, and honours the limit so a small page size exercises the cursor.
type fakeRecords struct {
	artifacts   []coreartifact.Artifact
	checkpoints []coretask.CheckpointPayloadRef
	traces      []coretask.RunTraceRef
	releases    []coreplugin.Release
	err         error
}

func page[T any](rows []T, key func(T) string, after string, limit int) []T {
	sort.Slice(rows, func(i, j int) bool { return key(rows[i]) < key(rows[j]) })
	var out []T
	for _, r := range rows {
		if key(r) > after && len(out) < limit {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeRecords) ListLiveArtifactsAfter(_ context.Context, after string, limit int) ([]coreartifact.Artifact, error) {
	return page(f.artifacts, func(a coreartifact.Artifact) string { return a.ID }, after, limit), f.err
}

func (f *fakeRecords) ListWorkspaceCheckpointPayloadsAfter(_ context.Context, after string, limit int) ([]coretask.CheckpointPayloadRef, error) {
	return page(f.checkpoints, func(c coretask.CheckpointPayloadRef) string { return c.CheckpointID }, after, limit), nil
}

func (f *fakeRecords) ListTaskRunTracesAfter(_ context.Context, after string, limit int) ([]coretask.RunTraceRef, error) {
	return page(f.traces, func(r coretask.RunTraceRef) string { return r.TaskRunID }, after, limit), nil
}

func (f *fakeRecords) ListPluginReleasesAfter(_ context.Context, afterName, afterVersion string, limit int) ([]coreplugin.Release, error) {
	return page(f.releases, func(r coreplugin.Release) string { return r.PluginName + "\x00" + r.Version },
		afterName+"\x00"+afterVersion, limit), nil
}

// fakeObjects is one bucket: bytes by key, and keys whose read fails with
// something other than absence.
type fakeObjects struct {
	objects map[string]string
	broken  map[string]bool
}

func (f *fakeObjects) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	if f.broken[key] {
		return nil, 0, errors.New("access denied")
	}
	b, ok := f.objects[key]
	if !ok {
		return nil, 0, apierr.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader([]byte(b))), int64(len(b)), nil
}

func (f *fakeObjects) Key(spaceID, sha256hex string) (string, error) {
	return path.Join(spaceID, "workspace/blobs/sha256", sha256hex), nil
}

func (f *fakeObjects) OpenArtifact(ctx context.Context, ref coreartifact.Ref) (io.ReadCloser, error) {
	rc, _, err := f.Open(ctx, path.Join("spaces", ref.SpaceID, "artifacts", ref.ArtifactID, "content"))
	return rc, err
}

func (f *fakeObjects) OpenTrace(ctx context.Context, ref coretask.RunTraceRef) (io.ReadCloser, error) {
	rc, _, err := f.Open(ctx, path.Join(ref.SpaceID, "tasks", ref.TaskID, ref.TaskRunID, "global", ref.TracePath))
	return rc, err
}

// fixture holds one resolved, one missing, and one altered reference of every
// kind, plus a checkpoint whose recorded key disagrees with the derivation and
// an artifact whose store refuses the read.
func fixture() (*fakeRecords, *fakeObjects) {
	objs := &fakeObjects{objects: map[string]string{}, broken: map[string]bool{}}
	recs := &fakeRecords{}

	put := func(key, body string) { objs.objects[key] = body }

	// Artifacts: a1 resolves, a2 is missing, a3 has different bytes, a4 is refused.
	for _, a := range []struct{ id, stored, recorded string }{
		{"a1", "hello", "hello"}, {"a2", "", "gone"}, {"a3", "tampered", "original"}, {"a4", "x", "x"},
	} {
		recs.artifacts = append(recs.artifacts, coreartifact.Artifact{
			ID: a.id, SpaceID: "s1", SHA256: digest(a.recorded), SizeBytes: int64(len(a.recorded)),
		})
		if a.stored != "" {
			put(path.Join("spaces/s1/artifacts", a.id, "content"), a.stored)
		}
	}
	objs.broken["spaces/s1/artifacts/a4/content"] = true

	// Checkpoints: c1 resolves, c2 is missing, c3 has different bytes, c4 records a foreign key.
	for _, c := range []struct{ id, stored, recorded string }{
		{"c1", "tar1", "tar1"}, {"c2", "", "tar2"}, {"c3", "tarX", "tar3"},
	} {
		key := path.Join("s1/workspace/blobs/sha256", digest(c.recorded))
		recs.checkpoints = append(recs.checkpoints, coretask.CheckpointPayloadRef{
			CheckpointID: c.id, SpaceID: "s1", StorageKey: key, PayloadSHA256: digest(c.recorded), SizeBytes: int64(len(c.recorded)),
		})
		if c.stored != "" {
			put(key, c.stored)
		}
	}
	put("old-prefix/s1/workspace/blobs/sha256/"+digest("tar4"), "tar4")
	recs.checkpoints = append(recs.checkpoints, coretask.CheckpointPayloadRef{
		CheckpointID: "c4", SpaceID: "s1", StorageKey: "old-prefix/s1/workspace/blobs/sha256/" + digest("tar4"),
		PayloadSHA256: digest("tar4"), SizeBytes: 4,
	})

	// Traces: r1 resolves, r2 is missing. A trace records no digest, so there
	// is no mismatch to find.
	recs.traces = []coretask.RunTraceRef{
		{SpaceID: "s1", TaskID: "t1", TaskRunID: "r1", TracePath: "traces/r1.jsonl"},
		{SpaceID: "s1", TaskID: "t1", TaskRunID: "r2", TracePath: "traces/r2.jsonl"},
	}
	put("s1/tasks/t1/r1/global/traces/r1.jsonl", "{}\n")

	// Plugin releases: p@1 resolves, p@2 is missing, p@3 has different bytes,
	// q@1 records a digest that is not a labelled sha256.
	for _, r := range []struct{ version, stored, recorded string }{
		{"1.0.0", "pkg1", "pkg1"}, {"2.0.0", "", "pkg2"}, {"3.0.0", "pkgX", "pkg3"},
	} {
		key := "plugins/p/sha256-" + digest(r.recorded) + ".tar.gz"
		recs.releases = append(recs.releases, coreplugin.Release{
			PluginName: "p", Version: r.version, Digest: "sha256:" + digest(r.recorded), ObjectKey: key, SizeBytes: int64(len(r.recorded)),
		})
		if r.stored != "" {
			put(key, r.stored)
		}
	}
	put("plugins/q/pkg.tar.gz", "q")
	recs.releases = append(recs.releases, coreplugin.Release{
		PluginName: "q", Version: "1.0.0", Digest: "md5:abc", ObjectKey: "plugins/q/pkg.tar.gz", SizeBytes: 1,
	})
	return recs, objs
}

func newChecker(recs *fakeRecords, objs *fakeObjects, checksums bool) (*Checker, map[string]Finding) {
	found := map[string]Finding{}
	return &Checker{
		Records: recs, Artifacts: objs, Checkpoints: objs, Traces: objs, Packages: objs,
		Checksums: checksums,
		// Two rows a page, so every kind crosses at least one page boundary.
		PageSize:  2,
		OnFinding: func(f Finding) { found[string(f.Kind)+"/"+f.ID] = f },
	}, found
}

func TestChecksumModeFindsMissingAndAlteredObjectsOfEveryKind(t *testing.T) {
	recs, objs := fixture()
	c, found := newChecker(recs, objs, true)
	report, err := c.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	want := map[string]Problem{
		"artifact/a2":             ProblemMissing,
		"artifact/a3":             ProblemMismatch,
		"artifact/a4":             ProblemUnreadable,
		"workspace_checkpoint/c2": ProblemMissing,
		"workspace_checkpoint/c3": ProblemMismatch,
		"workspace_checkpoint/c4": ProblemMismatch,
		"trace/r2":                ProblemMissing,
		"plugin_release/p@2.0.0":  ProblemMissing,
		"plugin_release/p@3.0.0":  ProblemMismatch,
		"plugin_release/q@1.0.0":  ProblemMismatch,
	}
	for key, problem := range want {
		got, ok := found[key]
		if !ok {
			t.Errorf("%s: no finding, want %s", key, problem)
			continue
		}
		if got.Problem != problem {
			t.Errorf("%s: problem %s (%s), want %s", key, got.Problem, got.Detail, problem)
		}
	}
	for key, f := range found {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected finding %s: %s %s", key, f.Problem, f.Detail)
		}
	}

	wantCounts := Report{
		KindArtifact:      {Checked: 4, Missing: 1, Mismatched: 1, Unreadable: 1},
		KindCheckpoint:    {Checked: 4, Missing: 1, Mismatched: 2},
		KindTrace:         {Checked: 2, Missing: 1},
		KindPluginRelease: {Checked: 4, Missing: 1, Mismatched: 2},
	}
	for kind, w := range wantCounts {
		if report[kind] != w {
			t.Errorf("%s counts = %+v, want %+v", kind, report[kind], w)
		}
	}
	if report.Findings() != len(want) {
		t.Errorf("Findings() = %d, want %d", report.Findings(), len(want))
	}
}

// Without --checksums the check proves existence only: an object with the
// wrong bytes is not read and so not reported, while a missing one still is.
func TestExistenceModeDoesNotReadContent(t *testing.T) {
	recs, objs := fixture()
	c, found := newChecker(recs, objs, false)
	report, err := c.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, key := range []string{"artifact/a3", "workspace_checkpoint/c3", "plugin_release/p@3.0.0"} {
		if f, ok := found[key]; ok {
			t.Errorf("%s reported %s without reading content", key, f.Problem)
		}
	}
	// Key and digest disagreements are read from the record, not the object.
	for _, key := range []string{"artifact/a2", "workspace_checkpoint/c2", "workspace_checkpoint/c4", "trace/r2", "plugin_release/p@2.0.0", "plugin_release/q@1.0.0"} {
		if _, ok := found[key]; !ok {
			t.Errorf("%s: no finding in existence mode", key)
		}
	}
	if report[KindArtifact].Checked != 4 || report[KindPluginRelease].Checked != 4 {
		t.Errorf("report = %+v, want every row checked", report)
	}
}

func TestSizeMismatchIsReportedBeforeDigest(t *testing.T) {
	recs := &fakeRecords{artifacts: []coreartifact.Artifact{{ID: "a1", SpaceID: "s1", SHA256: digest("abc"), SizeBytes: 3}}}
	objs := &fakeObjects{objects: map[string]string{"spaces/s1/artifacts/a1/content": "abcd"}}
	c, found := newChecker(recs, objs, true)
	if _, err := c.Run(t.Context()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	f := found["artifact/a1"]
	if f.Problem != ProblemMismatch || f.Detail != "object is 4 bytes, record says 3" {
		t.Errorf("finding = %+v", f)
	}
}

func TestCleanStoreHasNoFindings(t *testing.T) {
	recs := &fakeRecords{
		artifacts: []coreartifact.Artifact{{ID: "a1", SpaceID: "s1", SHA256: digest("x"), SizeBytes: 1}},
	}
	objs := &fakeObjects{objects: map[string]string{"spaces/s1/artifacts/a1/content": "x"}}
	var progress []Counts
	c, _ := newChecker(recs, objs, true)
	c.OnProgress = func(kind Kind, counts Counts, done bool) {
		if kind == KindArtifact && done {
			progress = append(progress, counts)
		}
	}
	report, err := c.Run(t.Context())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Findings() != 0 {
		t.Errorf("findings = %d, want 0", report.Findings())
	}
	if len(progress) != 1 || progress[0].Checked != 1 {
		t.Errorf("final artifact progress = %+v, want one report of 1 checked", progress)
	}
}

// A database that cannot be read is not a clean result: the check stops and
// says so rather than reporting zero findings over rows it never saw.
func TestUnreadableDatabaseFailsTheCheck(t *testing.T) {
	recs := &fakeRecords{err: errors.New("connection refused")}
	c, _ := newChecker(recs, &fakeObjects{}, false)
	if _, err := c.Run(t.Context()); err == nil {
		t.Fatal("Run succeeded over a database it could not read")
	}
}
