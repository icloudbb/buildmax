package desktop

import (
	"context"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/infra/git"
	"github.com/icloudbb/buildmax/internal/interface/slashcmd"
	tools "github.com/icloudbb/buildmax/internal/tool"
)

// --- Command registry ---

// GetSlashCommands returns the slash commands Desktop offers, from the shared
// registry so the CLI/TUI and Desktop stay in step. The frontend renders these
// in the "/" palette and dispatches each to the matching panel or action.
func (a *App) GetSlashCommands() []slashcmd.Command {
	return slashcmd.For(slashcmd.Desktop)
}

// --- Tools ---

type SlashToolEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	// Access is "read-only" or "write"; Action is the no-argument policy
	// outcome ("allow", "ask", "deny"), the category answer rather than a
	// promise about every call.
	Access string `json:"access,omitempty"`
	Action string `json:"action,omitempty"`
}

type SlashToolsResult struct {
	Tools []SlashToolEntry `json:"tools"`
}

// GetSlashTools returns the tools available to the given project's agent.
func (a *App) GetSlashTools(projectID string) (SlashToolsResult, error) {
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return SlashToolsResult{}, err
	}
	entries := ag.ToolEntries()
	out := make([]SlashToolEntry, len(entries))
	for i, e := range entries {
		out[i] = SlashToolEntry{Name: e.Name, Description: e.Description, Access: e.Access, Action: e.Action}
	}
	return SlashToolsResult{Tools: out}, nil
}

// --- Worktrees ---

type SlashWorktreeEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Branch   string `json:"branch,omitempty"`
	Current  bool   `json:"current"`
	Occupied bool   `json:"occupied"`
	// Holder names the session occupying the tree, when one is.
	Holder string `json:"holder,omitempty"`
}

type SlashWorktreesResult struct {
	// Available is false when this project is not a Git repository, so the
	// panel says so rather than showing an empty list.
	Available bool                 `json:"available"`
	Current   string               `json:"current,omitempty"`
	Worktrees []SlashWorktreeEntry `json:"worktrees"`
}

// GetSlashWorktrees lists the worktrees of the given project's repository.
func (a *App) GetSlashWorktrees(projectID string) (SlashWorktreesResult, error) {
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return SlashWorktreesResult{}, err
	}
	mgr := ag.Worktrees()
	if mgr == nil {
		return SlashWorktreesResult{Available: false}, nil
	}
	infos, err := mgr.List(context.Background())
	if err != nil {
		return SlashWorktreesResult{}, fmt.Errorf("list worktrees: %w", err)
	}
	out := SlashWorktreesResult{Available: true, Current: mgr.Current()}
	for _, w := range infos {
		out.Worktrees = append(out.Worktrees, SlashWorktreeEntry{
			Name:     w.Name,
			Path:     w.Path,
			Branch:   w.Branch,
			Current:  w.Current,
			Occupied: w.Occupied,
			Holder:   w.Holder,
		})
	}
	return out, nil
}

// --- Models ---

// The active model is Current on the result, not a per-entry flag: the frontend
// derives it by matching each entry against Current, which stays correct after a
// SetProjectModel without refetching the list.
type SlashModelEntry struct {
	Name          string `json:"name"`
	ProviderModel string `json:"provider_model,omitempty"`
}

type SlashModelsResult struct {
	Current string `json:"current"`
	// Managed and ServerURL say where every prompt in this session goes. It is
	// the app's mode, not a property of one model, so the picker states it once
	// above the list. See docs/design/client-modes.md.
	Managed   bool              `json:"managed"`
	ServerURL string            `json:"server_url,omitempty"`
	Models    []SlashModelEntry `json:"models"`
}

// GetSlashModels returns configured models and the active model for a project's
// conversation. The active model is the conversation's own: a switch is recorded
// per session, so a project's two conversations can sit on different models, and
// an empty sessionID (a chat not yet started) falls back to the app default.
func (a *App) GetSlashModels(projectID, sessionID string) (SlashModelsResult, error) {
	ag, err := a.resolveSessionApp(projectID, sessionID)
	if err != nil {
		return SlashModelsResult{}, err
	}
	current := ag.SessionModelName(sessionID)
	serverURL := ag.ManagedServerURL()
	configs := ag.ModelConfigs()
	models := make([]SlashModelEntry, len(configs))
	for i, c := range configs {
		models[i] = SlashModelEntry{
			Name:          c.Name,
			ProviderModel: c.ProviderModel,
		}
	}
	// Where prompts go is the app's mode, so it is reported once for the list
	// rather than per entry: in managed mode every one of them goes to the same
	// deployment, and in local mode none of them does.
	return SlashModelsResult{
		Current:   current,
		Managed:   serverURL != "",
		ServerURL: strings.TrimPrefix(strings.TrimPrefix(serverURL, "https://"), "http://"),
		Models:    models,
	}, nil
}

// SetProjectModel switches the model for a project's conversation.
//
// It records the choice in two places for two reasons: SetDefaultModel makes it
// the model this project's next new conversation starts on, and SetSessionModel
// writes it onto the conversation on screen so that conversation's next turn
// resolves to it. Without the second write the switch was lost — the app default
// is only a fallback, and an existing session already carries its own model,
// which won at request time. An empty sessionID is a chat not yet started; the
// default alone is enough, and it becomes that session's model when it is created.
func (a *App) SetProjectModel(projectID, sessionID, modelName string) error {
	if modelName == "" {
		return fmt.Errorf("model name required")
	}
	ag, err := a.resolveSessionApp(projectID, sessionID)
	if err != nil {
		return err
	}
	ag.SetDefaultModel(modelName)
	if sessionID != "" {
		if err := ag.SetSessionModel(sessionID, modelName); err != nil {
			return err
		}
	}
	return nil
}

// --- Skills ---

type SlashSkillEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Path        string `json:"path"`
}

type SlashSkillsResult struct {
	Skills []SlashSkillEntry `json:"skills"`
}

// GetSlashSkills returns all skills discovered for the given project.
func (a *App) GetSlashSkills(projectID string) (SlashSkillsResult, error) {
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return SlashSkillsResult{}, err
	}
	entries := ag.SkillEntries()
	out := make([]SlashSkillEntry, len(entries))
	for i, e := range entries {
		out[i] = SlashSkillEntry{Name: e.Name, Description: e.Description, Path: e.Path}
	}
	return SlashSkillsResult{Skills: out}, nil
}

// --- MCP ---

type SlashMCPServer struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	OK        bool   `json:"ok"`
	ToolCount int    `json:"tool_count"`
	Error     string `json:"error,omitempty"`
}

type SlashMCPResult struct {
	LoadError string           `json:"load_error,omitempty"`
	Servers   []SlashMCPServer `json:"servers"`
}

// GetSlashMCP returns the MCP server status for the given project.
func (a *App) GetSlashMCP(projectID string) (SlashMCPResult, error) {
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return SlashMCPResult{}, err
	}
	status := ag.MCPStatus()
	servers := make([]SlashMCPServer, len(status.Servers))
	for i, s := range status.Servers {
		errStr := ""
		if s.Err != nil {
			errStr = s.Err.Error()
		}
		servers[i] = SlashMCPServer{
			ID:        s.ID,
			Type:      s.Type,
			OK:        s.OK,
			ToolCount: s.ToolCount,
			Error:     errStr,
		}
	}
	return SlashMCPResult{LoadError: status.LoadError, Servers: servers}, nil
}

// --- Agents ---

type SlashAgentEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsBuiltin   bool   `json:"is_builtin"`
}

type SlashAgentsResult struct {
	Agents []SlashAgentEntry `json:"agents"`
}

// GetSlashAgents returns all agent types (builtin + user-defined) for the project.
func (a *App) GetSlashAgents(projectID string) (SlashAgentsResult, error) {
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return SlashAgentsResult{}, err
	}
	var agents []SlashAgentEntry
	for _, def := range tools.BuiltinSubAgentDefs() {
		agents = append(agents, SlashAgentEntry{
			Name:        def.Name,
			Description: def.Description,
			IsBuiltin:   true,
		})
	}
	for _, def := range ag.AgentDefs() {
		agents = append(agents, SlashAgentEntry{
			Name:        def.Name,
			Description: def.Description,
			IsBuiltin:   false,
		})
	}
	return SlashAgentsResult{Agents: agents}, nil
}

// --- Git branch ---

// GetGitBranch returns the current git branch for the given session's
// workspace, or an empty string if that folder is not a git repository.
func (a *App) GetGitBranch(projectID, sessionID string) (string, error) {
	root, err := resolveWorkspace(projectID, sessionID)
	if err != nil {
		return "", err
	}
	return git.CurrentBranch(root), nil
}

// GetWorkspaceDiff returns the current git-backed changed-file view for a session's workspace.
func (a *App) GetWorkspaceDiff(projectID, sessionID string) (git.WorkspaceDiff, error) {
	root, err := resolveWorkspace(projectID, sessionID)
	if err != nil {
		return git.WorkspaceDiff{}, err
	}
	return git.ReadWorkspace(context.Background(), root)
}

// ListCommits returns one page of the session workspace's HEAD history, newest
// first; skip is how many newer commits the caller has already shown.
func (a *App) ListCommits(projectID, sessionID string, skip, limit int) (git.CommitLog, error) {
	root, err := resolveWorkspace(projectID, sessionID)
	if err != nil {
		return git.CommitLog{}, err
	}
	return git.ListCommits(context.Background(), root, skip, limit)
}

// GetCommit lists the files one commit changed, without patches.
func (a *App) GetCommit(projectID, sessionID, sha string) (git.CommitDetail, error) {
	root, err := resolveWorkspace(projectID, sessionID)
	if err != nil {
		return git.CommitDetail{}, err
	}
	return git.ReadCommit(context.Background(), root, sha)
}

// GetCommitFileDiff returns one file's change in a commit, patch included.
func (a *App) GetCommitFileDiff(projectID, sessionID, sha, path string) (git.ChangedFile, error) {
	root, err := resolveWorkspace(projectID, sessionID)
	if err != nil {
		return git.ChangedFile{}, err
	}
	return git.ReadCommitFile(context.Background(), root, sha, path)
}
