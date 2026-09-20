package agentapp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/localproject"
	tools "github.com/icloudbb/buildmax/internal/tool"
	"github.com/icloudbb/buildmax/internal/util"
)

// memoryApp is the smallest AgentApp the memory seam reads through: a resolved
// Project and the catalog behind it.
func memoryApp(t *testing.T) *AgentApp {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	projects := NewProjectManager(filepath.Join(t.TempDir(), "projects"))
	project, err := projects.Resolve(context.Background(), dir)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}
	return &AgentApp{project: project, projects: projects, workspace: NewMovableRoot(dir)}
}

func writeMemory(t *testing.T, m *projectMemory, name, body string) agent.MemoryBody {
	t.Helper()
	got, err := m.Write(context.Background(), agent.MemoryUpsert{
		Name:        name,
		Description: "what " + name + " is about",
		Type:        string(localproject.MemoryTypeProject),
		Body:        body,
	})
	if err != nil {
		t.Fatalf("Write(%s): %v", name, err)
	}
	return got
}

func TestProjectMemoryIndexAndBodies(t *testing.T) {
	app := memoryApp(t)
	mem := app.projectMemoryFor("session-1")
	if mem == nil {
		t.Fatal("a run in a project has no memory store")
	}
	if got := mem.Index(); len(got.Entries) != 0 {
		t.Fatalf("fresh index = %+v, want empty", got)
	}

	writeMemory(t, mem, "merge-commit", "Use merge commits.\n\n**Why:** per-commit revert.")

	index := mem.Index()
	if len(index.Entries) != 1 || index.Entries[0].Name != "merge-commit" {
		t.Fatalf("index = %+v, want the one memory", index.Entries)
	}
	if index.ScopeID != app.project.ID {
		t.Errorf("ScopeID = %q, want the project id", index.ScopeID)
	}
	// The index carries a pointer, not the knowledge. That is what makes it
	// affordable to render on every call.
	if strings.Contains(index.Entries[0].Description, "per-commit revert") {
		t.Error("the index line carries the body")
	}

	bodies, missing, err := mem.Read(context.Background(), []string{"merge-commit", "absent"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(bodies) != 1 || !strings.Contains(bodies[0].Body, "per-commit revert") {
		t.Errorf("Read returned %+v, want the body", bodies)
	}
	if len(missing) != 1 || missing[0] != "absent" {
		t.Errorf("missing = %v, want the name that does not exist", missing)
	}
}

// A write from one session is visible to the next call of another, which is the
// whole reason the store lives on the Project rather than in a session.
func TestProjectMemoryIsSharedBetweenSessions(t *testing.T) {
	app := memoryApp(t)
	first := app.projectMemoryFor("session-1")
	second := app.projectMemoryFor("session-2")

	writeMemory(t, first, "learned", "Learned in session one.")

	index := second.Index()
	if len(index.Entries) != 1 || index.Entries[0].Name != "learned" {
		t.Errorf("the second session saw %+v, want the first's memory", index.Entries)
	}
}

// The run that has not read a memory cannot replace it, and the digest it is
// compared against never leaves the runtime.
func TestReplacingRequiresThisRunToHaveRead(t *testing.T) {
	app := memoryApp(t)
	author := app.projectMemoryFor("session-1")
	writeMemory(t, author, "merge-commit", "Use merge commits.")

	stranger := app.projectMemoryFor("session-2")
	_, err := stranger.Write(context.Background(), agent.MemoryUpsert{
		Name:        "merge-commit",
		Description: "d",
		Type:        string(localproject.MemoryTypeProject),
		Body:        "written blind",
	})
	if !errors.Is(err, localproject.ErrMemoryUnread) {
		t.Fatalf("Write = %v, want ErrMemoryUnread", err)
	}

	if _, _, err := stranger.Read(context.Background(), []string{"merge-commit"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if _, err := stranger.Write(context.Background(), agent.MemoryUpsert{
		Name:        "merge-commit",
		Description: "d",
		Type:        string(localproject.MemoryTypeProject),
		Body:        "merged deliberately",
	}); err != nil {
		t.Fatalf("write after reading: %v", err)
	}
}

// A run has read what it just wrote, so correcting it in the same turn does not
// need a round trip through the read tool.
func TestARunCanReplaceWhatItJustWrote(t *testing.T) {
	mem := memoryApp(t).projectMemoryFor("session-1")
	writeMemory(t, mem, "merge-commit", "Use merge commits.")
	writeMemory(t, mem, "merge-commit", "Use merge commits, never squash.")
}

// A body that moved under this run is a different failure from one never read,
// and gets a different answer.
func TestReplacingAStaleBodyIsRefused(t *testing.T) {
	app := memoryApp(t)
	reader := app.projectMemoryFor("session-1")
	writeMemory(t, reader, "merge-commit", "Use merge commits.")

	other := app.projectMemoryFor("session-2")
	if _, _, err := other.Read(context.Background(), []string{"merge-commit"}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	writeMemory(t, reader, "merge-commit", "Rewritten by the first session.")

	_, err := other.Write(context.Background(), agent.MemoryUpsert{
		Name:        "merge-commit",
		Description: "d",
		Type:        string(localproject.MemoryTypeProject),
		Body:        "from the older reader",
	})
	if !errors.Is(err, localproject.ErrMemoryConflict) {
		t.Fatalf("Write = %v, want ErrMemoryConflict", err)
	}
}

func TestDeleteForgetsWhatWasRead(t *testing.T) {
	mem := memoryApp(t).projectMemoryFor("session-1")
	writeMemory(t, mem, "stale", "No longer true.")

	if err := mem.Delete(context.Background(), "stale"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if len(mem.Index().Entries) != 0 {
		t.Error("the index still lists a deleted memory")
	}
	// Recreating is a create, not a replacement, so it needs no prior read --
	// but the run must not be credited with having read the old body either.
	writeMemory(t, mem, "stale", "True again, for a new reason.")
}

// Two separate reasons a run has no memory, and both produce the same nothing:
// a surface that never had a Project, and a user who turned it off.
func TestNoMemoryStoreWithoutAProjectOrWhenDisabled(t *testing.T) {
	if store := (&AgentApp{}).projectMemoryFor("session-1"); store != nil {
		t.Error("a projectless run has a memory store")
	}
	disabled := memoryApp(t)
	disabled.memoryDisabled = true
	if store := disabled.projectMemoryFor("session-1"); store != nil {
		t.Error("a run with memory turned off has a memory store")
	}
	var nilApp *AgentApp
	if store := nilApp.projectMemoryFor("session-1"); store != nil {
		t.Error("a nil app has a memory store")
	}
}

// A file left unusable by a hand edit is silently absent from every run until
// someone repairs it, so the surface says so at run start rather than only in
// doctor.
func TestMemoryStatusNamesSkippedFiles(t *testing.T) {
	app := memoryApp(t)
	mem := app.projectMemoryFor("session-1")
	writeMemory(t, mem, "good", "Still fine.")

	dir := filepath.Join(app.projects.Dir(), app.project.ID, "memory")
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := app.MemoryStatus()
	if report.Empty() {
		t.Fatal("the report says nothing about a file that is never loaded")
	}
	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "broken.md") {
		t.Errorf("report does not name the file: %q", lines)
	}
	// The good memory still renders; one bad file is not the whole store.
	if len(mem.Index().Entries) != 1 {
		t.Error("a skipped file stopped the other memories rendering")
	}
	if (&AgentApp{}).MemoryStatus().Empty() != true {
		t.Error("a projectless run reports a memory problem")
	}
}

// Registration follows the same rule as reading, and from the same condition: a
// run that may not look at the store must not be able to change it.
func TestMemoryToolsFollowWhetherTheRunHasMemory(t *testing.T) {
	tests := []struct {
		name string
		cfg  AppConfig
		want bool
	}{
		{name: "project memory on", cfg: AppConfig{EnableLocalProject: true}, want: true},
		{name: "memory turned off for this run", cfg: AppConfig{EnableLocalProject: true, DisableProjectMemory: true}},
		{name: "no local project at all", cfg: AppConfig{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.cfg
			cfg.WorkspaceDir = t.TempDir()
			app, err := NewAgentApp(cfg)
			if err != nil {
				t.Fatalf("NewAgentApp: %v", err)
			}
			t.Cleanup(func() { _ = app.Close() })

			found := map[string]bool{}
			for _, entry := range app.ToolEntries() {
				found[entry.Name] = true
			}
			for _, name := range []string{tools.ToolNameMemoryRead, tools.ToolNameMemoryWrite} {
				if found[name] != tt.want {
					t.Errorf("%s registered = %v, want %v", name, found[name], tt.want)
				}
			}
		})
	}
}

// A delegate carries neither tool, so no agent definition can name one.
func TestBaseToolsExcludeTheMemoryTools(t *testing.T) {
	base := buildBaseTools(nil, util.FixedRoot(t.TempDir()), stubTool{}, agent.NoopSandbox{}, "", nil, nil)
	for _, tl := range base {
		if tl.Name() == tools.ToolNameMemoryRead || tl.Name() == tools.ToolNameMemoryWrite {
			t.Fatalf("%s is in the base set, so a subagent definition can name it", tl.Name())
		}
	}
}

// The trace says which sources put a line in front of the model, and does it
// without copying any of them into a third file with a different retention.
func TestContextSourcesReportTheIndexWithoutQuotingIt(t *testing.T) {
	app := memoryApp(t)
	writeMemory(t, app.projectMemoryFor("session-1"), "merge-commit", "Use merge commits, never squash.")

	sources := app.contextSources(nil, []agent.PromptLayer{{Name: "runtime", Chars: 4000}})

	if sources.ProjectID != app.project.ID {
		t.Errorf("ProjectID = %q, want %q", sources.ProjectID, app.project.ID)
	}
	if sources.Workspace != app.workspace.Root() {
		t.Errorf("Workspace = %q, want the root this run used", sources.Workspace)
	}
	if len(sources.Memory) != 1 || sources.Memory[0].Name != "project_index" {
		t.Fatalf("Memory = %+v, want the index row", sources.Memory)
	}
	row := sources.Memory[0]
	if row.Entries != 1 || row.Chars == 0 {
		t.Errorf("index row = %+v, want a count and a size", row)
	}
	// Session notes and todos change every iteration, so a per-run count would
	// report the value at run start while reading as a fact about the run.
	for _, m := range sources.Memory {
		if strings.HasPrefix(m.Name, "session_") {
			t.Errorf("the run record carries per-iteration state: %+v", m)
		}
	}
}

func TestContextSourcesOmitAnEmptyIndex(t *testing.T) {
	app := memoryApp(t)
	sources := app.contextSources(nil, nil)
	if len(sources.Memory) != 0 {
		t.Errorf("Memory = %+v, want nothing for a project with no memories", sources.Memory)
	}
	if sources.HistoryProjection.CompactionPresent {
		t.Error("a run that compacted nothing reports a compaction")
	}
}

// §9.1: a store that cannot be read as a whole withdraws the index and both
// tools together. A run that cannot see what it holds must not be able to add
// to it, and a partial index would be worse than none.
func TestAnUnreadableStoreWithdrawsIndexAndTools(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a directory with no permissions")
	}
	workspace := t.TempDir()
	projectsDir := t.TempDir()
	project, err := NewProjectManager(projectsDir).Resolve(context.Background(), workspace)
	if err != nil {
		t.Fatalf("resolve project: %v", err)
	}
	dir := filepath.Join(projectsDir, project.ID, "memory")
	if err := os.MkdirAll(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	app := &AgentApp{
		project:           project,
		projects:          NewProjectManager(projectsDir),
		workspace:         NewMovableRoot(workspace),
		memoryUnavailable: "permission denied",
	}
	if app.memoryEnabled() {
		t.Error("an unreadable store still counts as enabled")
	}
	if app.projectMemoryFor("session-1") != nil {
		t.Error("an unreadable store still produced a memory seam")
	}
	report := app.MemoryStatus()
	if report.Unavailable == "" {
		t.Error("the surface is not told the store is unavailable")
	}
	if len(app.memoryIndexSourceInfo()) != 0 {
		t.Error("the trace claims an index the run never carried")
	}
}
