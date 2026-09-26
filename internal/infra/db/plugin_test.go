package db

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	"github.com/icloudbb/buildmax/internal/util"
)

// The store is the only implementation of the catalog contract, so a mismatch
// should fail here rather than when the routes are wired.
var _ pluginsvc.CatalogStore = (*Store)(nil)

// A report that will not decode costs the report, not the release: the bytes
// and their digest are still exactly what was published.
func TestToPluginReleaseSurvivesADamagedDocument(t *testing.T) {
	row := &pluginReleaseRow{
		PluginName: "code-review",
		Version:    "1.2.0",
		Digest:     "sha256:abc",
		Inspection: "{not json",
		Source:     "{also not json",
	}
	got := toPluginRelease(&pluginReleaseReadRow{Row: *row})
	if got.Version != "1.2.0" || got.Digest != "sha256:abc" {
		t.Errorf("release identity lost: %+v", got)
	}
	if len(got.Inspection.Skills) != 0 || got.Source.Commit != "" {
		t.Errorf("a damaged document should decode to nothing, got %+v", got)
	}
}

func TestToPluginReleaseDecodesDocuments(t *testing.T) {
	inspection, err := json.Marshal(coreplugin.Inspection{
		Skills:  []string{"review"},
		MCP:     []coreplugin.MCPServer{{ID: "github", Transport: "stdio", Executable: "npx"}},
		EnvRefs: []string{"GITHUB_TOKEN"},
	})
	if err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal(coreplugin.ReleaseSource{
		RemoteURL: "git@example.com:x.git", Commit: "abc123", Dirty: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	got := toPluginRelease(&pluginReleaseReadRow{Row: pluginReleaseRow{Inspection: string(inspection), Source: string(source)}})
	if len(got.Inspection.Skills) != 1 || got.Inspection.MCP[0].Executable != "npx" {
		t.Errorf("inspection = %+v", got.Inspection)
	}
	if !got.Source.Dirty || got.Source.Commit != "abc123" {
		t.Errorf("source = %+v", got.Source)
	}
}

func TestPluginArchivedAndYanked(t *testing.T) {
	if (coreplugin.Plugin{}).Archived() {
		t.Error("a live entry is not archived")
	}
	if !(coreplugin.Plugin{ArchivedAt: util.Ptr(time.Unix(1, 0).UTC())}).Archived() {
		t.Error("a retired entry is archived")
	}
	if (coreplugin.Release{}).Yanked() {
		t.Error("a published release is not yanked")
	}
	if !(coreplugin.Release{YankedAt: util.Ptr(time.Unix(1, 0).UTC())}).Yanked() {
		t.Error("a withdrawn release is yanked")
	}
}

// The rest needs a database. It follows the convention of the other store
// tests: skipped unless a DSN is configured.
func newPluginStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, ctx
}

func makeCatalogEntry(t *testing.T, s *Store, ctx context.Context, name string) *coreplugin.Plugin {
	t.Helper()
	_ = s.db.WithContext(ctx).Delete(&pluginReleaseRow{}, "plugin_name = ?", name)
	_ = s.db.WithContext(ctx).Delete(&pluginRow{}, "name = ?", name)
	entry, err := s.CreatePlugin(ctx, coreplugin.CreateInput{
		Name: name, DisplayName: "Code Review", Description: "Reviews.", CreatedBy: newTestUser(t, s, "plugin"),
	})
	if err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.WithContext(ctx).Delete(&pluginReleaseRow{}, "plugin_name = ?", name)
		_ = s.db.WithContext(ctx).Delete(&pluginRow{}, "name = ?", name)
	})
	return entry
}

func TestPluginCatalogLifecycle(t *testing.T) {
	s, ctx := newPluginStore(t)
	const name = "store-test-code-review"
	entry := makeCatalogEntry(t, s, ctx, name)
	if entry.Name != name {
		t.Fatalf("entry = %+v", entry)
	}

	if _, err := s.CreatePlugin(ctx, coreplugin.CreateInput{Name: name, CreatedBy: newTestUser(t, s, "plugin")}); !errors.Is(err, coreplugin.ErrNameTaken) {
		t.Errorf("duplicate name: err = %v, want ErrPluginNameTaken", err)
	}
	if got, err := s.GetPlugin(ctx, "store-test-absent"); err != nil || got != nil {
		t.Errorf("missing entry = %+v, %v; want nil, nil", got, err)
	}

	updated, err := s.UpdatePlugin(ctx, name, coreplugin.UpdateInput{DisplayName: "Renamed", Description: "New."})
	if err != nil {
		t.Fatalf("UpdatePlugin: %v", err)
	}
	if updated.DisplayName != "Renamed" || updated.Description != "New." {
		t.Errorf("updated = %+v", updated)
	}
	if _, err := s.UpdatePlugin(ctx, "store-test-absent", coreplugin.UpdateInput{}); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("updating a missing entry: err = %v, want ErrNotFound", err)
	}

	if err := s.SetPluginArchived(ctx, name, true); err != nil {
		t.Fatalf("SetPluginArchived: %v", err)
	}
	if !containsPlugin(listPlugins(t, s, ctx, true), name) {
		t.Error("an archived entry should still list when asked for")
	}
	if containsPlugin(listPlugins(t, s, ctx, false), name) {
		t.Error("an archived entry should be out of the default catalog")
	}
	// Archiving hides and refuses, it does not delete.
	if _, err := s.CreatePluginRelease(ctx, coreplugin.CreateReleaseInput{
		PluginName: name, Version: "1.0.0", Digest: "sha256:a", ObjectKey: "k", PublishedBy: newTestUser(t, s, "publisher"),
	}); !errors.Is(err, coreplugin.ErrArchived) {
		t.Errorf("publishing to an archived entry: err = %v, want ErrPluginArchived", err)
	}
	if err := s.SetPluginArchived(ctx, name, false); err != nil {
		t.Fatalf("restore: %v", err)
	}
}

func TestPluginReleaseIsImmutable(t *testing.T) {
	s, ctx := newPluginStore(t)
	const name = "store-test-immutable"
	makeCatalogEntry(t, s, ctx, name)

	in := coreplugin.CreateReleaseInput{
		PluginName:         name,
		Version:            "1.2.0",
		MinBuildmaxVersion: "0.9.0",
		Digest:             "sha256:abc",
		ObjectKey:          "plugins/store-test-immutable/1.2.0.tar.gz",
		SizeBytes:          4096,
		Inspection:         coreplugin.Inspection{Skills: []string{"review"}, EnvRefs: []string{"GITHUB_TOKEN"}},
		Source:             coreplugin.ReleaseSource{RemoteURL: "git@example.com:x.git", Commit: "abc123"},
		PublishedBy:        newTestUser(t, s, "publisher"),
	}
	rel, err := s.CreatePluginRelease(ctx, in)
	if err != nil {
		t.Fatalf("CreatePluginRelease: %v", err)
	}
	if rel.PluginName != name {
		t.Fatalf("release = %+v", rel)
	}

	// Identical bytes are still a second publication of one version.
	if _, err := s.CreatePluginRelease(ctx, in); !errors.Is(err, coreplugin.ErrVersionExists) {
		t.Errorf("republishing: err = %v, want ErrPluginVersionExists", err)
	}
	if _, err := s.CreatePluginRelease(ctx, coreplugin.CreateReleaseInput{
		PluginName: "store-test-absent", Version: "1.0.0", Digest: "sha256:a", PublishedBy: newTestUser(t, s, "publisher"),
	}); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("publishing to a missing entry: err = %v, want ErrNotFound", err)
	}

	got, err := s.GetPluginRelease(ctx, name, "1.2.0")
	if err != nil {
		t.Fatalf("GetPluginRelease: %v", err)
	}
	if got.MinBuildmaxVersion != "0.9.0" || got.SizeBytes != 4096 {
		t.Errorf("release = %+v", got)
	}
	if len(got.Inspection.Skills) != 1 || got.Source.Commit != "abc123" {
		t.Errorf("documents did not round trip: %+v", got)
	}
	if missing, err := s.GetPluginRelease(ctx, name, "9.9.9"); err != nil || missing != nil {
		t.Errorf("missing release = %+v, %v; want nil, nil", missing, err)
	}
}

func TestPluginReleaseYank(t *testing.T) {
	s, ctx := newPluginStore(t)
	const name = "store-test-yank"
	makeCatalogEntry(t, s, ctx, name)
	for _, v := range []string{"1.0.0", "1.1.0"} {
		if _, err := s.CreatePluginRelease(ctx, coreplugin.CreateReleaseInput{
			PluginName: name, Version: v, Digest: "sha256:" + v, ObjectKey: v, PublishedBy: newTestUser(t, s, "publisher"),
		}); err != nil {
			t.Fatalf("publish %s: %v", v, err)
		}
	}

	admin := newTestUser(t, s, "yanker")
	if err := s.YankPluginRelease(ctx, name, "1.1.0", admin, "broken hook"); err != nil {
		t.Fatalf("YankPluginRelease: %v", err)
	}
	got, err := s.GetPluginRelease(ctx, name, "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Yanked() || got.YankedBy != admin || got.YankedReason != "broken hook" {
		t.Errorf("yanked release = %+v", got)
	}

	// Yanking again keeps the first withdrawal, which is the fact a past
	// installation is explained by.
	if err := s.YankPluginRelease(ctx, name, "1.1.0", newTestUser(t, s, "yanker2"), "again"); err != nil {
		t.Errorf("yanking twice should not error: %v", err)
	}
	again, err := s.GetPluginRelease(ctx, name, "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if again.YankedBy != admin || again.YankedAt == nil || !again.YankedAt.Equal(*got.YankedAt) {
		t.Errorf("second yank rewrote the record: %+v", again)
	}
	if err := s.YankPluginRelease(ctx, name, "9.9.9", admin, ""); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("yanking a missing release: err = %v, want ErrNotFound", err)
	}

	// Choosing between releases needs the version arithmetic, so the store
	// hands back everything including what was withdrawn.
	releases, err := s.ListPluginReleases(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 2 {
		t.Fatalf("listed %d releases, want both", len(releases))
	}
	if releases[0].Version != "1.0.0" {
		t.Errorf("releases should be oldest first, got %s", releases[0].Version)
	}
}

func listPlugins(t *testing.T, s *Store, ctx context.Context, includeArchived bool) []coreplugin.Plugin {
	t.Helper()
	got, err := s.ListPlugins(ctx, includeArchived)
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	return got
}

func containsPlugin(list []coreplugin.Plugin, name string) bool {
	for _, p := range list {
		if p.Name == name {
			return true
		}
	}
	return false
}
