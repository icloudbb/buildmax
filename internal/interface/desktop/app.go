// Package desktop implements the BuildMax desktop app (Wails) and is used by cmd/buildmax-desktop.
package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/core/localproject"
	"github.com/icloudbb/buildmax/internal/core/session"
	launchstore "github.com/icloudbb/buildmax/internal/infra/locallaunchpadstore"
	"github.com/icloudbb/buildmax/internal/infra/localprojectstore"
	histstore "github.com/icloudbb/buildmax/internal/infra/localschedulehistorystore"
	schedstore "github.com/icloudbb/buildmax/internal/infra/localschedulestore"
	snapstore "github.com/icloudbb/buildmax/internal/infra/localterminalsnapshotstore"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/interface/client"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// SessionDetail is the session payload returned to the frontend for display.
type SessionDetail struct {
	ID        string        `json:"id"`
	Title     string        `json:"title,omitempty"`
	CreatedAt string        `json:"created_at"`
	Messages  []llm.Message `json:"messages,omitempty"`
}

// ReplyPayload is returned when a desktop prompt completes successfully.
type ReplyPayload struct {
	Reply                 string `json:"reply"`
	SessionID             string `json:"session_id"`
	ContextTokens         int    `json:"context_tokens"`
	ContextWindow         int    `json:"context_window"`
	PromptTokens          int    `json:"prompt_tokens"`
	CompletionTokens      int    `json:"completion_tokens"`
	TotalPromptTokens     int    `json:"total_prompt_tokens"`
	TotalCompletionTokens int    `json:"total_completion_tokens"`
	// Cache counts are the cached parts of the prompt counts above, not extra
	// tokens to add to them.
	CacheReadTokens       int `json:"cache_read_tokens"`
	CacheWriteTokens      int `json:"cache_write_tokens"`
	TotalCacheReadTokens  int `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int `json:"total_cache_write_tokens"`
}

// Every streamed event below carries the SessionID of the run it belongs to, so
// the frontend can route it to the right chat tab when several sessions run at
// once. Only ReplyPayload's id comes from the run result; the rest are stamped
// from the run's live session (see desktopRun).

// StreamDeltaPayload is one chunk of assistant text (event desktop/stream-delta).
type StreamDeltaPayload struct {
	SessionID string `json:"session_id"`
	Delta     string `json:"delta"`
}

// LLMStartPayload marks the start of a model call (event desktop/llm-start).
type LLMStartPayload struct {
	SessionID string `json:"session_id"`
}

// SessionAdoptedPayload is emitted once, at the start of a run that began as a
// new chat (empty session id), carrying the real id the run created (event
// desktop/session-adopted). It fires before any of the run's stream events, and
// only new-chat runs emit it, so the frontend's pending new-chat tab can adopt
// the id unambiguously even while other sessions stream concurrently.
type SessionAdoptedPayload struct {
	SessionID string `json:"session_id"`
}

// StreamErrorPayload is emitted when streaming fails (event desktop/stream-error).
type StreamErrorPayload struct {
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// ToolStartPayload is emitted when a tool call begins executing.
type ToolStartPayload struct {
	SessionID  string `json:"session_id"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Args       string `json:"args"`
}

// ToolEndPayload is emitted when a tool call finishes or is denied.
type ToolEndPayload struct {
	SessionID  string `json:"session_id"`
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	DurationMs int64  `json:"duration_ms,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	Denied     bool   `json:"denied,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type RunStatusPayload struct {
	SessionID             string `json:"session_id"`
	ContextTokens         int    `json:"context_tokens"`
	ContextWindow         int    `json:"context_window"`
	PromptTokens          int    `json:"prompt_tokens"`
	CompletionTokens      int    `json:"completion_tokens"`
	TotalPromptTokens     int    `json:"total_prompt_tokens"`
	TotalCompletionTokens int    `json:"total_completion_tokens"`
	CacheReadTokens       int    `json:"cache_read_tokens"`
	CacheWriteTokens      int    `json:"cache_write_tokens"`
	TotalCacheReadTokens  int    `json:"total_cache_read_tokens"`
	TotalCacheWriteTokens int    `json:"total_cache_write_tokens"`
}

// MessageDequeuedPayload is emitted just before a queued prompt starts its own turn
// (event desktop/message-dequeued), so the transcript can show it as sent.
type MessageDequeuedPayload struct {
	SessionID string   `json:"session_id"`
	Prompt    string   `json:"prompt"`
	Queued    []string `json:"queued"`
}

// MessageBlockedPayload is emitted when a hook refuses a queued prompt
// (event desktop/message-blocked). It reports one message, not the end of the
// run — the run carries on with what it already had.
type MessageBlockedPayload struct {
	SessionID string   `json:"session_id"`
	Prompt    string   `json:"prompt"`
	Reason    string   `json:"reason"`
	Queued    []string `json:"queued"`
}

// TurnDigestPayload is what the finished turn is worth telling the user, and
// nothing the model will read again (event desktop/turn-digest). It is emitted
// once per turn rather than with stream-done, because a run that drains a queue
// runs several turns and each one's recap describes only itself.
//
// It deliberately does not travel in the message list: stream-done reloads the
// thread from the session, and neither of these is in the session.
type TurnDigestPayload struct {
	SessionID string `json:"session_id"`
	// Recap is a short account of what the turn did, or "" when it earned none.
	Recap string `json:"recap"`
	// Suggestion is the answer the user is likely about to give, or "" when the
	// turn did not end by asking them anything.
	Suggestion string `json:"suggestion"`
}

const (
	eventStreamDelta     = "desktop/stream-delta"
	eventStreamDone      = "desktop/stream-done"
	eventStreamError     = "desktop/stream-error"
	eventSessionAdopted  = "desktop/session-adopted"
	eventLLMStart        = "desktop/llm-start"
	eventToolStart       = "desktop/tool-start"
	eventToolEnd         = "desktop/tool-end"
	eventRunStatus       = "desktop/run-status"
	eventMessageDequeued = "desktop/message-dequeued"
	eventMessageBlocked  = "desktop/message-blocked"
	eventTurnDigest      = "desktop/turn-digest"
)

// uiEmitter delivers one event to the frontend.
//
// The indirection exists because Wails' own events interface lives in one of
// its internal packages: nothing outside that module can implement it, so a
// test cannot stand in for the frontend unless the seam is on this side.
// Production always uses wailsEmit.
type uiEmitter func(ctx context.Context, name string, data any)

func wailsEmit(ctx context.Context, name string, data any) {
	runtime.EventsEmit(ctx, name, data)
}

// App holds desktop application state and implements Wails lifecycle hooks.
// Each project gets its own AgentApp and ApprovalHandler instance, created lazily on first use.
type App struct {
	ctx              context.Context
	mu               sync.Mutex
	agentApps        map[string]*agentapp.AgentApp      // keyed by project ID
	approvalHandlers map[string]*DesktopApprovalHandler // keyed by project ID
	// scheduleApps are AgentApps for scheduled fires, keyed by working directory.
	// A scheduled task targets a directory, not a project, so its runs are built
	// with EnableLocalProject off: sessions are stamped with no project (invisible
	// in the project session list) and no project catalog row is created.
	scheduleApps map[string]*agentapp.AgentApp
	// scheduler serializes runs per session (see runKey): one run per session in
	// flight at a time, prompts submitted meanwhile queued and drained as their
	// own turns, the session held for a run's life, and cancellation that
	// discards the queue. Different sessions run concurrently.
	scheduler *agentapp.RunScheduler
	// pendingJobEvents park requested background deliveries per
	// project+session (see deliveryKey) until the frontend pulls them with
	// DeliverNextJobEvent. Lazily initialized.
	pendingJobEvents map[string][]agentapp.BackgroundEvent
	// emit sends an event to the frontend. See uiEmitter.
	emit uiEmitter
	// terminals owns the interactive shell strands shown as terminal tabs. See
	// the desktop-terminal-tabs proposal.
	terminals *terminalManager
	// schedules stores the local scheduled tasks. Lazily opened (see
	// ensureScheduleStore) so a test that never touches schedules needs no
	// BUILDMAX_HOME.
	schedules *schedstore.FileStore
	// scheduleRuns stores each scheduled fire's run history — the only link from a
	// task to the projectless sessions it created. Lazily opened.
	scheduleRuns *histstore.FileStore
	// stopSched ends the resident schedule tick loop; nil when it is not running.
	stopSched chan struct{}
	// launchpad stores the user's custom quick-launch entries. Lazily opened (see
	// ensureLaunchpadStore) so a test that never touches it needs no BUILDMAX_HOME.
	launchpad *launchstore.FileStore
	// terminalSnapshots stores each terminal tab's last serialized buffer so a
	// restart can restore its visible contents. Lazily opened.
	terminalSnapshots *snapstore.FileStore
}

// NewApp returns a new App instance.
func NewApp() *App {
	a := &App{
		agentApps:        make(map[string]*agentapp.AgentApp),
		approvalHandlers: make(map[string]*DesktopApprovalHandler),
		scheduleApps:     make(map[string]*agentapp.AgentApp),
		scheduler:        agentapp.NewRunScheduler(),
		emit:             wailsEmit,
	}
	// The terminal manager is Wails-agnostic; bind it to the app's emitter, which
	// resolves the live context at call time (nil before Startup).
	a.terminals = newTerminalManager(func(name string, data any) {
		a.mu.Lock()
		ctx := a.ctx
		a.mu.Unlock()
		if ctx == nil {
			return
		}
		a.emit(ctx, name, data)
	})
	return a
}

// Startup is called by Wails when the app is starting.
// AgentApp instances are created lazily on first use per project.
func (a *App) Startup(ctx context.Context) {
	a.mu.Lock()
	a.ctx = ctx
	a.mu.Unlock()
	_ = config.DataDir()
	// The tick loop fires local scheduled tasks while the app is open; it needs
	// the live context set above so a fired run can stream to the frontend.
	a.startScheduler()
}

// Shutdown closes all per-project AgentApp instances and cancels any in-flight runs.
func (a *App) Shutdown(_ context.Context) {
	// Reap every shell strand so none is orphaned past the Desktop process.
	a.terminals.closeAll()
	// Stop the schedule tick loop so no fire starts a run mid-teardown.
	a.stopScheduler()
	// Cancel every in-flight run before taking a.mu: the scheduler holds its own
	// lock, and a StartEvent pop callback takes a.mu under it, so a.mu must never
	// be held while calling into the scheduler.
	a.scheduler.CancelAll()
	a.mu.Lock()
	apps := a.agentApps
	schedApps := a.scheduleApps
	a.agentApps = make(map[string]*agentapp.AgentApp)
	a.approvalHandlers = make(map[string]*DesktopApprovalHandler)
	a.scheduleApps = make(map[string]*agentapp.AgentApp)
	a.mu.Unlock()
	for _, ag := range apps {
		_ = ag.Close()
	}
	for _, ag := range schedApps {
		_ = ag.Close()
	}
}

// agentAppForProject returns the cached AgentApp for the given project, creating
// it on first use. Concurrent calls for the same project are safe; at most one
// instance is created per project.
func (a *App) agentAppForProject(projectID string) (*agentapp.AgentApp, error) {
	a.mu.Lock()
	ag, ok := a.agentApps[projectID]
	a.mu.Unlock()
	if ok {
		return ag, nil
	}

	proj, err := projectManager().Store().Get(context.Background(), projectID)
	if err != nil {
		return nil, err
	}

	source, err := auth.ResolveModelSource(context.Background())
	if err != nil {
		return nil, err
	}
	handler := newDesktopApprovalHandler(a, projectID)
	// Desktop opens a Project at its default workspace, so one Project is one
	// root here and the cache can be keyed by Project alone. A relink that
	// moves the default workspace is picked up on the next launch; nothing
	// changes a root under a running window.
	ag, err = agentapp.NewAgentApp(agentapp.AppConfig{
		WorkspaceDir:         proj.DefaultWorkspace,
		EnableMCP:            true,
		Policy:               agent.AllowAllPolicy(),
		ModelEntries:         source.Entries,
		DefaultModel:         source.Default,
		ManagedServerURL:     source.ServerURL,
		ManagedToken:         auth.TokenForServer,
		ArtifactPublisher:    auth.ArtifactPublisherForSession(),
		Surface:              coregw.CallSurfaceDesktop,
		EnableBackgroundJobs: true,
		EnableLocalProject:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("init agent for project %q: %w", proj.Name, err)
	}

	a.mu.Lock()
	// Another goroutine may have created it while we were initializing.
	if existing, ok := a.agentApps[projectID]; ok {
		a.mu.Unlock()
		_ = ag.Close()
		return existing, nil
	}
	a.agentApps[projectID] = ag
	a.approvalHandlers[projectID] = handler
	a.mu.Unlock()
	if jobs := ag.Jobs(); jobs != nil {
		// The pump exits when Close releases the subscription in Shutdown.
		a.pumpJobEvents(projectID, jobs)
	}
	return ag, nil
}

// agentAppForDir returns the cached AgentApp a scheduled task fires in, keyed by
// its working directory and created on first use. Unlike agentAppForProject it
// builds with EnableLocalProject off, so a fire's session carries no project
// (staying out of the project session list) and running in an arbitrary folder
// does not register it in the project catalog. Concurrent calls for the same
// directory create at most one instance.
func (a *App) agentAppForDir(dir string) (*agentapp.AgentApp, error) {
	a.mu.Lock()
	ag, ok := a.scheduleApps[dir]
	a.mu.Unlock()
	if ok {
		return ag, nil
	}

	source, err := auth.ResolveModelSource(context.Background())
	if err != nil {
		return nil, err
	}
	ag, err = agentapp.NewAgentApp(agentapp.AppConfig{
		WorkspaceDir:         dir,
		EnableMCP:            true,
		Policy:               agent.AllowAllPolicy(),
		ModelEntries:         source.Entries,
		DefaultModel:         source.Default,
		ManagedServerURL:     source.ServerURL,
		ManagedToken:         auth.TokenForServer,
		ArtifactPublisher:    auth.ArtifactPublisherForSession(),
		Surface:              coregw.CallSurfaceDesktop,
		EnableBackgroundJobs: true,
		EnableLocalProject:   false,
	})
	if err != nil {
		return nil, fmt.Errorf("init agent for %q: %w", dir, err)
	}

	a.mu.Lock()
	if existing, ok := a.scheduleApps[dir]; ok {
		a.mu.Unlock()
		_ = ag.Close()
		return existing, nil
	}
	a.scheduleApps[dir] = ag
	a.mu.Unlock()
	return ag, nil
}

// hostForDir resolves the AgentApp a scheduled fire runs in. A scheduled run is
// unattended, so it carries no approval handler: its AllowAllPolicy runs tools
// without prompting, and there is no user at the keyboard to answer anyway.
func (a *App) hostForDir(dir string) agentapp.HostFunc {
	return func() (agentapp.RunHost, error) {
		return a.agentAppForDir(dir)
	}
}

// sessionWorkspaceDir is the directory a projectless session runs in: its own
// recorded workspace (stamped after its first turn), falling back to the user's
// home. Scheduled sessions carry no project, so their host is resolved by
// directory rather than by a Project catalog lookup.
func (a *App) sessionWorkspaceDir(sessionID string) string {
	if sessionID != "" {
		if loaded, err := sessionManager().Load(sessionID, session.LoadMetaOnly); err == nil && loaded.Meta.Workspace != "" {
			return loaded.Meta.Workspace
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "."
}

// resolveSessionApp returns the AgentApp for a session-scoped operation: the
// project's app when a project is given, otherwise the app hosting the session's
// own directory. This lets the chat bindings (send, run status, model, compact)
// serve a projectless scheduled session with the same code the project chat uses.
func (a *App) resolveSessionApp(projectID, sessionID string) (*agentapp.AgentApp, error) {
	if projectID != "" {
		return a.agentAppForProject(projectID)
	}
	return a.agentAppForDir(a.sessionWorkspaceDir(sessionID))
}

// hostForSession is resolveSessionApp as a scheduler HostFunc. For a projectless
// session it binds no approval handler: the directory host runs AllowAllPolicy,
// so no tool call stops to ask.
func (a *App) hostForSession(projectID, sessionID string, lc *desktopRun) agentapp.HostFunc {
	if projectID != "" {
		return a.hostForProject(projectID, lc)
	}
	dir := a.sessionWorkspaceDir(sessionID)
	return func() (agentapp.RunHost, error) {
		return a.agentAppForDir(dir)
	}
}

// --- Project bindings ---

// projectManager is the shared local Project catalog. Desktop owns no Project
// record of its own: CLI and Desktop opened on one repository have to be the
// same Project, or the two surfaces would list different sessions and, once
// project memory lands, read different memory. See
// docs/design/local-project-memory.md §11.5.
func projectManager() *agentapp.ProjectManager {
	return agentapp.NewProjectManager(config.ProjectsDir())
}

// ListProjects returns the local Projects, most recently used first.
func (a *App) ListProjects() ([]localproject.Summary, error) {
	rows, err := projectManager().Store().List(context.Background())
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	if rows == nil {
		return []localproject.Summary{}, nil
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].LastUsedAt.After(rows[j].LastUsedAt) })
	return rows, nil
}

// OpenFolderDialog opens a native directory picker and returns the selected path.
func (a *App) OpenFolderDialog() (string, error) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return "", fmt.Errorf("app not ready")
	}
	path, err := runtime.OpenDirectoryDialog(ctx, runtime.OpenDialogOptions{
		Title: "Select Project Folder",
	})
	if err != nil {
		return "", err
	}
	return path, nil
}

// OpenProject resolves a folder to its local Project, registering one the first
// time that folder is opened.
//
// It replaces a create binding because adding a folder is no longer how a
// Project comes to exist: a worktree of a repository Desktop already knows
// opens that repository's Project rather than a second one beside it. A name is
// applied when given, so opening a folder and calling it something reads the
// way it looks.
func (a *App) OpenProject(folderPath, name string) (*localproject.Summary, error) {
	if folderPath == "" {
		return nil, fmt.Errorf("folder path required")
	}
	ctx := context.Background()
	proj, err := projectManager().Resolve(ctx, folderPath)
	if err != nil {
		return nil, fmt.Errorf("open project: %w", err)
	}
	if name != "" && name != proj.Name {
		if err := projectManager().Store().Update(ctx, proj.ID, localproject.Update{Name: &name}); err != nil {
			return nil, fmt.Errorf("name project: %w", err)
		}
		proj.Name = name
	}
	summary := proj.Summarize()
	return &summary, nil
}

// RenameProject updates the name of a project.
func (a *App) RenameProject(id, newName string) error {
	if newName == "" {
		return fmt.Errorf("project name required")
	}
	return projectManager().Store().Update(context.Background(), id, localproject.Update{Name: &newName})
}

// DeleteProject removes a Project. Its sessions go only when deleteSessions
// says so.
//
// A Project and its sessions are separate things to destroy, so removing one
// never silently takes the other: a Project that still has sessions is refused
// with the count, and the caller comes back having told the user what will be
// lost. See docs/design/local-project-memory.md §15.
func (a *App) DeleteProject(id string, deleteSessions bool) error {
	ctx := context.Background()
	if _, err := projectManager().Store().Get(ctx, id); err != nil {
		return err
	}
	held, err := a.projectSessionIDs(id)
	if err != nil {
		return err
	}
	switch {
	case len(held) == 0:
	case !deleteSessions:
		return fmt.Errorf("project %s still has %d session(s); clear them first or confirm deleting them", id, len(held))
	default:
		if _, err := sessionManager().DeleteByProject(id); err != nil {
			return fmt.Errorf("delete project sessions: %w", err)
		}
	}
	if err := projectManager().Store().Delete(ctx, id); err != nil {
		return err
	}
	a.mu.Lock()
	ag := a.agentApps[id]
	delete(a.agentApps, id)
	delete(a.approvalHandlers, id)
	a.mu.Unlock()
	if ag != nil {
		_ = ag.Close()
	}
	return nil
}

// touchProjectLastUsed advances a Project's recency stamp. Best-effort: errors
// are logged and dropped so they cannot interrupt a chat reply.
func touchProjectLastUsed(projectID string) {
	err := projectManager().Store().Update(context.Background(), projectID, localproject.Update{TouchLastUsed: true})
	if err != nil {
		slog.Warn("touch project last used failed", "project_id", projectID, "err", err)
	}
}

// ProjectNotices are the things the runtime wants said once when a project is
// opened: a project registered for a directory that may be a moved repository,
// and memory files that will be silently absent from every run until repaired.
//
// A source missing for a whole session without anyone being told is the failure
// this prevents, and a desktop user never runs `buildmax doctor`. Building the
// runtime is what produces them, so this is also what warms it.
func (a *App) ProjectNotices(projectID string) ([]string, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID required")
	}
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return nil, err
	}
	notices := ag.StartupNotices("buildmax project relink " + projectID)
	if notices == nil {
		notices = []string{}
	}
	return notices, nil
}

// MemoryEntry is one of a project's memories as the desktop shows it.
//
// It carries the body, unlike the index a model is given: a person opening
// their own memories is not paying a per-call context cost, and the body is
// where the reason lives. The frontend still shows one at a time, because a
// list of twenty bodies is not a list.
type MemoryEntry struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Body        string `json:"body"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	VerifiedAt  string `json:"verified_at,omitempty"`
}

// MemorySkipped is a memory file that could not be used. It is reported rather
// than omitted: such a memory is silently absent from every run until someone
// repairs it.
type MemorySkipped struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// ProjectMemoryPayload is what a project remembers, for the memory drawer.
type ProjectMemoryPayload struct {
	ProjectName string `json:"project_name"`
	Directory   string `json:"directory"`
	// IndexChars is what the index costs on every model call and IndexBudget
	// what it may cost. That pair is what a person prunes against, not the
	// count.
	IndexChars  int `json:"index_chars"`
	IndexBudget int `json:"index_budget"`
	// Unavailable distinguishes a store that could not be read from one with
	// nothing in it.
	Unavailable string          `json:"unavailable,omitempty"`
	Memories    []MemoryEntry   `json:"memories"`
	Skipped     []MemorySkipped `json:"skipped,omitempty"`
}

// ProjectMemory returns what this project remembers.
//
// Read-only. Editing a memory from the desktop is the phase 3 surface in
// docs/design/local-project-memory.md §11.5, and needs the refusal path a
// digest-checked write can take; the files are editable in the meantime and the
// directory is returned so a person can open them.
func (a *App) ProjectMemory(projectID string) (ProjectMemoryPayload, error) {
	if projectID == "" {
		return ProjectMemoryPayload{}, fmt.Errorf("project ID required")
	}
	ctx := context.Background()
	manager := projectManager()
	project, err := manager.Store().Get(ctx, projectID)
	if err != nil {
		return ProjectMemoryPayload{}, err
	}
	overview := manager.MemoryOverviewFor(ctx, project)

	payload := ProjectMemoryPayload{
		ProjectName: project.Name,
		Directory:   filepath.Join(config.ProjectsDir(), project.ID, localprojectstore.MemoryDir),
		IndexChars:  overview.IndexChars,
		IndexBudget: overview.IndexBudget,
		Unavailable: overview.Unavailable,
		Memories:    make([]MemoryEntry, 0, len(overview.Memories)),
	}
	for _, m := range overview.Memories {
		entry := MemoryEntry{
			Name:        m.Name,
			Type:        string(m.Type),
			Description: m.Description,
			Body:        m.Body,
		}
		if !m.UpdatedAt.IsZero() {
			entry.UpdatedAt = m.UpdatedAt.Format(time.RFC3339)
		}
		if m.VerifiedAt != nil {
			entry.VerifiedAt = m.VerifiedAt.Format("2006-01-02")
		}
		payload.Memories = append(payload.Memories, entry)
	}
	for _, s := range overview.Skipped {
		payload.Skipped = append(payload.Skipped, MemorySkipped{File: s.File, Reason: s.Reason})
	}
	return payload, nil
}

// projectSessionIDs lists the sessions a Project still owns.
func (a *App) projectSessionIDs(projectID string) ([]string, error) {
	rows, err := sessionManager().List()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	var ids []string
	for _, row := range rows {
		if row.ProjectID == projectID {
			ids = append(ids, row.ID)
		}
	}
	return ids, nil
}

// --- Session bindings ---

// sessionManager is the store behind every session binding below. Desktop has
// no long-lived agent for these: they list, rename, and delete sessions that
// belong to whichever project window is open, so each call goes through a
// manager over the one sessions root.
func sessionManager() *agentapp.SessionManager {
	return agentapp.NewSessionManager(config.SessionsDir())
}

// ListSessions returns all sessions across all projects.
func (a *App) ListSessions() ([]session.ItemSummary, error) {
	entries, err := sessionManager().List()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	if entries == nil {
		entries = []session.ItemSummary{}
	}
	return entries, nil
}

func (a *App) RenameSession(sessionID, title string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID required")
	}
	return sessionManager().Rename(sessionID, title)
}

func (a *App) DeleteSession(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session ID required")
	}
	return sessionManager().Delete(sessionID)
}

func (a *App) SetSessionPinned(sessionID string, pinned bool) error {
	if sessionID == "" {
		return fmt.Errorf("session ID required")
	}
	return sessionManager().SetPinned(sessionID, pinned)
}

func (a *App) ClearProjectSessions(projectID string) ([]string, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project ID required")
	}
	if _, err := projectManager().Store().Get(context.Background(), projectID); err != nil {
		return nil, err
	}
	return sessionManager().DeleteByProject(projectID)
}

// GetSession loads one session by ID and returns it for display.
func (a *App) GetSession(sessionID string) (SessionDetail, error) {
	if sessionID == "" {
		return SessionDetail{}, fmt.Errorf("session ID required")
	}
	// Read-only: displaying a session must not take its writer lock, or
	// opening the detail view would lock out the window that is running it.
	loaded, err := sessionManager().Load(sessionID, session.LoadFull)
	if err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return SessionDetail{}, fmt.Errorf("session not found: %s", sessionID)
		}
		return SessionDetail{}, fmt.Errorf("load session: %w", err)
	}
	display := make([]llm.Message, 0, len(loaded.State.Messages))
	for _, m := range loaded.State.Messages {
		if m.Role == "system" {
			continue
		}
		display = append(display, m)
	}
	return SessionDetail{
		ID:        loaded.Meta.ID,
		Title:     loaded.Meta.Title,
		CreatedAt: loaded.Meta.CreatedAt.Format(time.RFC3339),
		Messages:  display,
	}, nil
}

func (a *App) GetRunStatus(projectID, sessionID string) (RunStatusPayload, error) {
	if projectID == "" && sessionID == "" {
		return RunStatusPayload{}, fmt.Errorf("session required")
	}
	ag, err := a.resolveSessionApp(projectID, sessionID)
	if err != nil {
		return RunStatusPayload{}, err
	}
	// Read-only: a status view has to work while a turn holds the session, so
	// it must not be the thing that takes the writer lock.
	sess, err := ag.ReadSession(sessionID)
	if err != nil {
		return RunStatusPayload{}, fmt.Errorf("read session: %w", err)
	}
	st, err := ag.EstimateRunUsage(sess)
	if err != nil {
		return RunStatusPayload{}, err
	}
	return RunStatusPayload{
		ContextTokens:         st.ContextTokens,
		ContextWindow:         st.ContextWindow,
		PromptTokens:          st.PromptTokens,
		CompletionTokens:      st.CompletionTokens,
		TotalPromptTokens:     st.TotalPromptTokens,
		TotalCompletionTokens: st.TotalCompletionTokens,
		CacheReadTokens:       st.CacheReadTokens,
		CacheWriteTokens:      st.CacheWriteTokens,
		TotalCacheReadTokens:  st.TotalCacheReadTokens,
		TotalCacheWriteTokens: st.TotalCacheWriteTokens,
	}, nil
}

// --- Mode and auth bindings ---

// AuthStatus is who is signed in, and therefore which mode the app is in.
//
// There is no mode field. A login is the mode: with one the app is managed and
// its models come from that server, without one it is local and they come from
// settings.yaml. Anything remembered alongside the credentials would be a second
// source of truth for one fact. See docs/design/client-modes.md section 3.
type AuthStatus struct {
	LoggedIn  bool   `json:"logged_in"`
	ServerURL string `json:"server_url,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Email     string `json:"email,omitempty"`
	Name      string `json:"name,omitempty"`
	// Expired means the stored login no longer works. The app stays in managed
	// mode and refuses to run rather than quietly using local models, which
	// would send prompts somewhere the user did not choose. Signing in again or
	// signing out are the two ways out — see docs/design/client-modes.md
	// section 8.
	Expired bool `json:"expired,omitempty"`
	// ExpiredDetail is what to tell the user, set only when Expired.
	ExpiredDetail string `json:"expired_detail,omitempty"`
}

// GetAuthStatus reports the stored login, if there is one, and whether it still
// works. The check runs here rather than on the first prompt so the answer
// arrives while the user is still deciding what to do.
func (a *App) GetAuthStatus() (*AuthStatus, error) {
	// The stored credentials, not auth.Info: Info reports a spent login as
	// signed out, and the app would then open in local mode on its own rather
	// than saying the session ended.
	creds, err := auth.StoredLogin()
	if err != nil {
		return nil, fmt.Errorf("load auth: %w", err)
	}
	if creds == nil {
		return &AuthStatus{}, nil
	}
	status := &AuthStatus{
		LoggedIn:  true,
		ServerURL: creds.ServerURL,
		UserID:    creds.UserID,
		Email:     creds.Email,
		Name:      creds.Name,
	}
	if _, err := auth.ResolveModelSource(context.Background()); err != nil {
		status.Expired = true
		status.ExpiredDetail = err.Error()
	}
	return status, nil
}

// GetDefaultServerURL is what the sign-in form starts with. It reads the same
// settings.yaml key `buildmax login` does, so the two entry points offer the
// same address instead of the Desktop insisting on a local server someone has
// already configured away from.
func (a *App) GetDefaultServerURL() string {
	s, err := config.LoadSettings()
	if err == nil && s.ServerURL != "" {
		return s.ServerURL
	}
	return client.DefaultServerURL
}

// RequestOTP calls the server's OTP endpoint.
func (a *App) RequestOTP(serverURL, email, intent string) error {
	c := client.NewClient(serverURL)
	return c.RequestOTP(context.Background(), email, intent)
}

// DoLoginWithPassword authenticates with a password and saves credentials on
// success. This is the everyday path.
func (a *App) DoLoginWithPassword(serverURL, email, password string) (*AuthStatus, error) {
	c := client.NewClient(serverURL)
	lr, err := c.LoginWithPassword(context.Background(), email, password, "desktop")
	if err != nil {
		return nil, err
	}
	return a.saveLogin(serverURL, lr)
}

// DoLogin authenticates with a single-use login code and saves credentials on
// success. It is the recovery path: claiming a new account, or getting back in
// after a forgotten password.
func (a *App) DoLogin(serverURL, email, otp string) (*AuthStatus, error) {
	c := client.NewClient(serverURL)
	lr, err := c.Login(context.Background(), email, otp, "desktop")
	if err != nil {
		return nil, err
	}
	return a.saveLogin(serverURL, lr)
}

func (a *App) saveLogin(serverURL string, lr *client.LoginResponse) (*AuthStatus, error) {
	creds := &auth.Credentials{
		ServerURL:    serverURL,
		Token:        lr.Access(),
		RefreshToken: lr.RefreshToken,
		UserID:       lr.User.ID,
		Email:        lr.User.Email,
		Name:         lr.User.Name,
	}
	if err := auth.SaveCredentials(creds); err != nil {
		return nil, fmt.Errorf("save credentials: %w", err)
	}
	return &AuthStatus{
		LoggedIn:  true,
		ServerURL: serverURL,
		UserID:    lr.User.ID,
		Email:     lr.User.Email,
		Name:      lr.User.Name,
	}, nil
}

// Logout clears stored credentials and revokes the session on the server.
//
// A server that cannot be reached is not a failed logout: the credentials are
// gone from this machine either way, and returning an error would leave the UI
// showing someone as signed in when they are not.
// Signing out is what returns the app to local mode: the credentials are the
// mode, so removing them is the whole switch.
func (a *App) Logout() error {
	if err := auth.LogoutAndRevoke(); err != nil {
		slog.Warn("logout could not revoke the session on the server", "err", err)
	}
	return nil
}

// --- Chat bindings ---

// RespondApproval is called by the frontend when the user answers a tool
// approval prompt. projectID must match the project that triggered the
// desktop/approval-request event. decision is "once", "session", or "deny";
// anything else denies, so a frontend that falls out of step fails closed.
func (a *App) RespondApproval(projectID string, decision string) {
	a.mu.Lock()
	handler := a.approvalHandlers[projectID]
	a.mu.Unlock()
	if handler == nil {
		return
	}
	switch decision {
	case "once":
		handler.respond(agent.ApprovalAllowOnce)
	case "session":
		handler.respond(agent.ApprovalAllowSession)
	default:
		handler.respond(agent.ApprovalDeny)
	}
}

// desktopStreamSink emits each delta to the frontend, tagged with the run's live
// session id so the frontend routes it to the right chat tab. session is read at
// emit time because a brand-new chat only learns its id once the run starts.
type desktopStreamSink struct {
	ctx     context.Context
	emit    uiEmitter
	session func() string
}

func (s *desktopStreamSink) OnDelta(delta string) {
	s.emit(s.ctx, eventStreamDelta, &StreamDeltaPayload{SessionID: s.session(), Delta: delta})
}

// desktopEventSink returns an agent.EventSink that forwards tool events to the frontend via Wails events.
// session reports the run's live session id (stamped on every payload for tab
// routing); queued reports the prompts still waiting behind the running turn,
// read when a queued prompt joins it so the frontend can show what remains.
func desktopEventSink(emit uiEmitter, ctx context.Context, session func() string, queued func() []string) func(agent.Event) {
	return func(e agent.Event) {
		sid := session()
		switch e.Kind {
		case agent.EventLLMStart:
			emit(ctx, eventLLMStart, &LLMStartPayload{SessionID: sid})
			emit(ctx, eventRunStatus, &RunStatusPayload{
				SessionID:        sid,
				ContextTokens:    e.ContextTokens,
				ContextWindow:    e.ContextWindow,
				PromptTokens:     e.PromptTokens,
				CompletionTokens: e.CompletionTokens,
				CacheReadTokens:  e.CacheReadTokens,
				CacheWriteTokens: e.CacheWriteTokens,
			})
		case agent.EventLLMEnd:
			emit(ctx, eventRunStatus, &RunStatusPayload{
				SessionID:        sid,
				PromptTokens:     e.PromptTokens,
				CompletionTokens: e.CompletionTokens,
				CacheReadTokens:  e.CacheReadTokens,
				CacheWriteTokens: e.CacheWriteTokens,
			})
		case agent.EventToolStart:
			emit(ctx, eventToolStart, &ToolStartPayload{SessionID: sid, ToolCallID: e.ToolCallID, ToolName: e.ToolName, Args: e.ToolArgs})
		case agent.EventToolEnd:
			emit(ctx, eventToolEnd, &ToolEndPayload{
				SessionID:  sid,
				ToolCallID: e.ToolCallID,
				ToolName:   e.ToolName,
				DurationMs: e.ToolDuration.Milliseconds(),
				IsError:    strings.HasPrefix(e.ToolResult, "error:"),
			})
		case agent.EventToolDenied:
			emit(ctx, eventToolEnd, &ToolEndPayload{SessionID: sid, ToolCallID: e.ToolCallID, ToolName: e.ToolName, IsError: true, Denied: true, Reason: e.DenyReason})
		case agent.EventUserInput:
			// A queued prompt joined the running turn: it is sent now, not waiting.
			emit(ctx, eventMessageDequeued, &MessageDequeuedPayload{SessionID: sid, Prompt: e.Content, Queued: queued()})
		case agent.EventUserInputBlocked:
			// Its own event, not stream-error: the run is still going, and the
			// frontend ends the run on stream-error.
			emit(ctx, eventMessageBlocked, &MessageBlockedPayload{
				SessionID: sid,
				Prompt:    e.Content,
				Reason:    e.DenyReason,
				Queued:    queued(),
			})
		}
	}
}

// runKey is the scheduler key for one session's run. Keying by project+session
// (not project alone) lets different sessions in a project run concurrently; a
// brand-new chat keys on an empty session id, so new chats still serialize until
// one earns an id. It matches deliveryKey's shape on purpose.
func runKey(projectID, sessionID string) string { return projectID + "\x00" + sessionID }

// QueuedMessages returns the prompts waiting behind a session's in-flight run,
// oldest first. The frontend reads it when it switches back to a chat tab whose
// queue events it was not mounted for.
func (a *App) QueuedMessages(projectID, sessionID string) []string {
	if projectID == "" && sessionID == "" {
		return nil
	}
	return a.scheduler.Queued(runKey(projectID, sessionID))
}

// SendMessageStream runs a prompt in the given project and session with streaming.
// It returns immediately and emits desktop/stream-delta, then desktop/stream-done
// or desktop/stream-error, each tagged with the run's session id. sessionID may
// be empty to start a new session.
//
// At most one run per session may be in flight (see runKey). A prompt submitted
// while that session's run is active is queued and runs as its own turn once the
// current one finishes; the return value is that prompt's 1-based position in the
// queue, and 0 when the prompt started a run of its own. Runs in different
// sessions of the same project proceed concurrently — the user owns keeping them
// from clobbering each other (e.g. a per-session worktree).
func (a *App) SendMessageStream(projectID, sessionID, prompt string) (int, error) {
	if prompt == "" {
		return 0, fmt.Errorf("prompt required")
	}
	// A projectless session (a scheduled run continued from the Schedules view)
	// carries an id, so its host is resolved by directory; only a project chat
	// starts a brand-new session with no id.
	if projectID == "" && sessionID == "" {
		return 0, fmt.Errorf("project ID required")
	}
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return 0, fmt.Errorf("app not ready")
	}
	lc := &desktopRun{app: a, ctx: ctx, projectID: projectID, sessionID: sessionID, key: runKey(projectID, sessionID), wasNew: sessionID == "", touchLastUsed: projectID != ""}
	// The scheduler resolves the host only if it commits to a run; a prompt that
	// queues behind an in-flight run resolves nothing, preserving the old
	// "queue without re-resolving the host" behaviour. A resolution failure is
	// reported as this call's error.
	return a.scheduler.Submit(ctx, lc.key, sessionID, prompt, a.hostForSession(projectID, sessionID, lc), lc)
}

// hostForProject resolves the project's AgentApp and binds its approval handler
// to lc, for the scheduler to call when it starts a run.
func (a *App) hostForProject(projectID string, lc *desktopRun) agentapp.HostFunc {
	return func() (agentapp.RunHost, error) {
		ag, err := a.agentAppForProject(projectID)
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		lc.handler = a.approvalHandlers[projectID]
		a.mu.Unlock()
		return ag, nil
	}
}

// emitTurnDigest sends the finished turn's recap and suggestion, if it produced
// either. Silence when it produced neither: an event carrying two empty strings
// would make the frontend clear a recap the user is still reading.
func (a *App) emitTurnDigest(ctx context.Context, sessionID string, out agentapp.RunResult) {
	if out.Digest.Empty() {
		return
	}
	a.emit(ctx, eventTurnDigest, &TurnDigestPayload{
		SessionID:  sessionID,
		Recap:      out.Digest.Recap,
		Suggestion: out.Digest.Suggestion,
	})
}

func replyPayload(out agentapp.RunResult) *ReplyPayload {
	return &ReplyPayload{
		Reply:                 out.Reply,
		SessionID:             out.SessionID,
		ContextTokens:         out.ContextTokens,
		ContextWindow:         out.ContextWindow,
		PromptTokens:          out.PromptTokens,
		CompletionTokens:      out.CompletionTokens,
		TotalPromptTokens:     out.TotalPromptTokens,
		TotalCompletionTokens: out.TotalCompletionTokens,
		CacheReadTokens:       out.CacheReadTokens,
		CacheWriteTokens:      out.CacheWriteTokens,
		TotalCacheReadTokens:  out.TotalCacheReadTokens,
		TotalCacheWriteTokens: out.TotalCacheWriteTokens,
	}
}

// CancelRun cancels the in-flight run for the given project and session, if any.
// Cancellation is cooperative: the agent loop returns the partial assistant
// reply produced so far and emits desktop/stream-done as a normal completion.
// Calling CancelRun when no run is in flight is a no-op.
//
// Stopping also discards anything queued behind the run. Those prompts were
// written for work the user has just called off; delivering them afterwards would
// restart it in their name.
func (a *App) CancelRun(projectID, sessionID string) error {
	if projectID == "" && sessionID == "" {
		return fmt.Errorf("session required")
	}
	a.scheduler.Cancel(runKey(projectID, sessionID))
	return nil
}
