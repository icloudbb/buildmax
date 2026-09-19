// Package tool provides concrete agent tools. Every runtime gets Read, Write,
// Edit, Glob, Grep, Bash, WebFetch, TodoWrite, NoteWrite, Skill, Task, and the
// MCP gateway tools; UploadArtifact, Worktree, MemoryRead, MemoryWrite, JobList,
// JobOutput, JobStop, and Monitor are registered only where the surface can
// serve them, as the constants below say. Reaching a space Issue is no longer a
// tool: an Agent reads and reports on its Issue by running `buildmax issue`
// through Bash, per docs/design/agent-bridge-cli.md.
package tool

import "github.com/icloudbb/buildmax/internal/core/agent"

// Tool name constants — single source of truth for every tool's Name(). Use camelCase for LLM-facing names.
const (
	ToolNameRead      = "Read"
	ToolNameWrite     = "Write"
	ToolNameEdit      = "Edit"
	ToolNameGlob      = "Glob"
	ToolNameGrep      = "Grep"
	ToolNameBash      = "Bash"
	ToolNameWebFetch  = "WebFetch"
	ToolNameTodoWrite = "TodoWrite"
	ToolNameNoteWrite = "NoteWrite"
	ToolNameSkill     = "Skill"
	ToolNameTask      = "Task"
	// ToolNameUploadArtifact is registered only where the surface has an
	// artifact service; a session with none does not offer it at all.
	ToolNameUploadArtifact = "UploadArtifact"
	// ToolNameWorktree is registered only where a session may move its own
	// workspace root — the CLI and TUI — and never inside subagents, which
	// share the parent's root for the length of their run.
	ToolNameWorktree = "Worktree"
	// The memory tools are registered only on a local primary run whose session
	// belongs to a Project and whose user has not turned memory off. A subagent
	// gets neither, and no index either: it is the highest-volume run in a
	// session, so it would pay the resident cost most often, and a parent that
	// needs it to know something can say so in the delegated task. They are
	// named for the operation rather than the scope, matching NoteWrite and
	// TodoWrite; a second scope would be an argument, not a second pair of
	// tools. See docs/design/local-project-memory.md §9.3 and §9.4.
	ToolNameMemoryRead  = agent.ToolNameMemoryRead
	ToolNameMemoryWrite = agent.ToolNameMemoryWrite
	// The Job tools and Monitor are registered only where local background
	// jobs are enabled (TUI and Desktop), and never inside subagents.
	ToolNameJobList   = "JobList"
	ToolNameJobOutput = "JobOutput"
	ToolNameJobStop   = "JobStop"
	ToolNameMonitor   = "Monitor"
)
