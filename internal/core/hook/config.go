// Package hook holds the shape of a hooks configuration block and the naming
// its events and transports use.
//
// It is the contract half of the pair whose implementation is
// internal/infra/hook, the same split as internal/core/llm and
// internal/infra/llm. It sits in core because two callers that cannot share
// anything higher both need it: loading a file for a run, and inspecting an
// uploaded plugin package before publishing it, which internal/server does and
// which may not import internal/config.
package hook

import "gopkg.in/yaml.v3"

// Event keys recognised in settings.yaml under "hooks". The values match
// agent.HookEvent so callers can pass them through directly.
const (
	EventSessionStart       = "SessionStart"
	EventSessionEnd         = "SessionEnd"
	EventUserPromptSubmit   = "UserPromptSubmit"
	EventPreToolUse         = "PreToolUse"
	EventPostToolUse        = "PostToolUse"
	EventPostToolUseFailure = "PostToolUseFailure"
	EventNotification       = "Notification"
	EventPreCompact         = "PreCompact"
	EventPostCompact        = "PostCompact"
	EventSubagentStart      = "SubagentStart"
	EventSubagentStop       = "SubagentStop"
	EventStop               = "Stop"
	EventStopFailure        = "StopFailure"
	EventWorktreeCreate     = "WorktreeCreate"
	EventWorktreeRemove     = "WorktreeRemove"
	EventCwdChanged         = "CwdChanged"
)

// DefaultTimeoutSecs is the per-hook execution timeout when a hook entry
// does not set its own timeout. Long enough for formatters/linters; short
// enough that a hung script does not block the agent for a full LLM turn.
const DefaultTimeoutSecs = 30

// Hook transport types. Match the canonical names used by Claude Code so
// scripts and operator docs port directly.
const (
	TypeCommand = "command"  // shell command on stdin/stdout
	TypeHTTP    = "http"     // HTTP POST with JSON body
	TypeMCP     = "mcp_tool" // MCP tool invocation
	TypePrompt  = "prompt"   // single-turn LLM prompt
)

// DefaultType is the transport assumed when an Entry omits Type. A hook that
// names only a command is the common case, so command is the default and that
// entry needs no Type field.
const DefaultType = TypeCommand

// Entry is one configured hook invocation. The Type discriminator
// selects which transport-specific fields are read; an entry with no Type
// defaults to TypeCommand, the common case.
//
// Matcher is a regular expression evaluated against the tool name for
// PreToolUse / PostToolUse; an empty Matcher matches every invocation. For
// non-tool events the field is ignored.
//
// Timeout is per-hook in seconds. Zero means DefaultTimeoutSecs.
//
// Per-type fields are kept on one flat struct so a Entry round-trips
// cleanly through YAML/JSON without a custom unmarshaller. A driver only
// reads the fields relevant to its Type; the rest are zero values.
type Entry struct {
	// Type selects the transport: "command", "http", "mcp_tool", "prompt".
	// Empty defaults to TypeCommand.
	Type    string `mapstructure:"type"     json:"type,omitempty"     yaml:"type,omitempty"`
	Matcher string `mapstructure:"matcher"  json:"matcher,omitempty"  yaml:"matcher,omitempty"`
	Timeout int    `mapstructure:"timeout"  json:"timeout,omitempty"  yaml:"timeout,omitempty"`

	// Command transport: shell command to execute via "sh -c" (Unix) or
	// "cmd /C" (Windows). Stdin receives the HookInput JSON.
	Command string `mapstructure:"command"  json:"command,omitempty"  yaml:"command,omitempty"`

	// HTTP transport: POSTs the HookInput JSON to URL. Headers values may
	// contain "$VAR" / "${VAR}" references; only env vars whitelisted in
	// AllowedEnv may be interpolated.
	URL        string            `mapstructure:"url"          json:"url,omitempty"          yaml:"url,omitempty"`
	Headers    map[string]string `mapstructure:"headers"      json:"headers,omitempty"      yaml:"headers,omitempty"`
	AllowedEnv []string          `mapstructure:"allowed_env"  json:"allowed_env,omitempty"  yaml:"allowed_env,omitempty"`

	// MCP transport: invoke Tool on Server with Input. Input values may
	// contain "${field}" references resolved against the HookInput payload.
	Server string         `mapstructure:"server"  json:"server,omitempty"  yaml:"server,omitempty"`
	Tool   string         `mapstructure:"tool"    json:"tool,omitempty"    yaml:"tool,omitempty"`
	Input  map[string]any `mapstructure:"input"   json:"input,omitempty"   yaml:"input,omitempty"`

	// Prompt transport: run a single-turn prompt against Model (empty uses
	// the default fast model). The literal "$ARGUMENTS" inside Prompt is
	// replaced with the HookInput JSON.
	Prompt string `mapstructure:"prompt"  json:"prompt,omitempty"  yaml:"prompt,omitempty"`
	Model  string `mapstructure:"model"   json:"model,omitempty"   yaml:"model,omitempty"`
}

// ResolvedType returns Entry.Type, defaulting to TypeCommand when empty.
func (e Entry) ResolvedType() string {
	if e.Type == "" {
		return DefaultType
	}
	return e.Type
}

// Config is the "hooks" block of settings.yaml. Per CLAUDE.md §6.1 the
// keys are snake_case; the values still resolve to the canonical HookEvent
// names listed above.
type Config struct {
	SessionStart       []Entry `mapstructure:"session_start"          json:"session_start,omitempty"          yaml:"session_start,omitempty"`
	SessionEnd         []Entry `mapstructure:"session_end"            json:"session_end,omitempty"            yaml:"session_end,omitempty"`
	UserPromptSubmit   []Entry `mapstructure:"user_prompt_submit"     json:"user_prompt_submit,omitempty"     yaml:"user_prompt_submit,omitempty"`
	PreToolUse         []Entry `mapstructure:"pre_tool_use"           json:"pre_tool_use,omitempty"           yaml:"pre_tool_use,omitempty"`
	PostToolUse        []Entry `mapstructure:"post_tool_use"          json:"post_tool_use,omitempty"          yaml:"post_tool_use,omitempty"`
	PostToolUseFailure []Entry `mapstructure:"post_tool_use_failure"  json:"post_tool_use_failure,omitempty"  yaml:"post_tool_use_failure,omitempty"`
	Notification       []Entry `mapstructure:"notification"           json:"notification,omitempty"           yaml:"notification,omitempty"`
	PreCompact         []Entry `mapstructure:"pre_compact"            json:"pre_compact,omitempty"            yaml:"pre_compact,omitempty"`
	PostCompact        []Entry `mapstructure:"post_compact"           json:"post_compact,omitempty"           yaml:"post_compact,omitempty"`
	SubagentStart      []Entry `mapstructure:"subagent_start"         json:"subagent_start,omitempty"         yaml:"subagent_start,omitempty"`
	SubagentStop       []Entry `mapstructure:"subagent_stop"          json:"subagent_stop,omitempty"          yaml:"subagent_stop,omitempty"`
	Stop               []Entry `mapstructure:"stop"                   json:"stop,omitempty"                   yaml:"stop,omitempty"`
	StopFailure        []Entry `mapstructure:"stop_failure"           json:"stop_failure,omitempty"           yaml:"stop_failure,omitempty"`
	WorktreeCreate     []Entry `mapstructure:"worktree_create"        json:"worktree_create,omitempty"        yaml:"worktree_create,omitempty"`
	WorktreeRemove     []Entry `mapstructure:"worktree_remove"        json:"worktree_remove,omitempty"        yaml:"worktree_remove,omitempty"`
	CwdChanged         []Entry `mapstructure:"cwd_changed"            json:"cwd_changed,omitempty"            yaml:"cwd_changed,omitempty"`
}

// EventNames lists every event in dispatch order, for a surface that has to
// enumerate them.
func EventNames() []string {
	return []string{
		EventSessionStart, EventSessionEnd, EventUserPromptSubmit,
		EventPreToolUse, EventPostToolUse, EventPostToolUseFailure,
		EventNotification, EventPreCompact, EventPostCompact,
		EventSubagentStart, EventSubagentStop,
		EventStop, EventStopFailure,
		EventWorktreeCreate, EventWorktreeRemove, EventCwdChanged,
	}
}

// Entries returns the configured hooks for the named event, in declared order.
// Unknown events return nil.
func (h Config) Entries(event string) []Entry {
	switch event {
	case EventSessionStart:
		return h.SessionStart
	case EventSessionEnd:
		return h.SessionEnd
	case EventUserPromptSubmit:
		return h.UserPromptSubmit
	case EventPreToolUse:
		return h.PreToolUse
	case EventPostToolUse:
		return h.PostToolUse
	case EventPostToolUseFailure:
		return h.PostToolUseFailure
	case EventNotification:
		return h.Notification
	case EventPreCompact:
		return h.PreCompact
	case EventPostCompact:
		return h.PostCompact
	case EventSubagentStart:
		return h.SubagentStart
	case EventSubagentStop:
		return h.SubagentStop
	case EventStop:
		return h.Stop
	case EventStopFailure:
		return h.StopFailure
	case EventWorktreeCreate:
		return h.WorktreeCreate
	case EventWorktreeRemove:
		return h.WorktreeRemove
	case EventCwdChanged:
		return h.CwdChanged
	default:
		return nil
	}
}

// IsEmpty reports whether the configuration has no hooks at all.
func (h Config) IsEmpty() bool {
	return len(h.SessionStart) == 0 &&
		len(h.SessionEnd) == 0 &&
		len(h.UserPromptSubmit) == 0 &&
		len(h.PreToolUse) == 0 &&
		len(h.PostToolUse) == 0 &&
		len(h.PostToolUseFailure) == 0 &&
		len(h.Notification) == 0 &&
		len(h.PreCompact) == 0 &&
		len(h.PostCompact) == 0 &&
		len(h.SubagentStart) == 0 &&
		len(h.SubagentStop) == 0 &&
		len(h.Stop) == 0 &&
		len(h.StopFailure) == 0 &&
		len(h.WorktreeCreate) == 0 &&
		len(h.WorktreeRemove) == 0 &&
		len(h.CwdChanged) == 0
}

// EachEntry applies fn to every entry, in dispatch order.
//
// The pointer is the point: expansion rewrites entries in place, and inspection
// walks the same set. Both would otherwise repeat this thirteen-way list and
// drift the first time an event was added.
func (h *Config) EachEntry(fn func(*Entry)) {
	for _, slice := range h.eventSlices() {
		for i := range *slice {
			fn(&(*slice)[i])
		}
	}
}

func (h *Config) eventSlices() []*[]Entry {
	return []*[]Entry{
		&h.SessionStart, &h.SessionEnd, &h.UserPromptSubmit,
		&h.PreToolUse, &h.PostToolUse, &h.PostToolUseFailure,
		&h.Notification, &h.PreCompact, &h.PostCompact,
		&h.SubagentStart, &h.SubagentStop,
		&h.Stop, &h.StopFailure,
		&h.WorktreeCreate, &h.WorktreeRemove, &h.CwdChanged,
	}
}

// ParseConfig decodes a hooks.yaml document.
//
// This is for a caller holding bytes rather than a path — inspecting an
// uploaded package, for one. A run loads through internal/config, which reads
// the same block out of settings.yaml as well.
func ParseConfig(data []byte) (Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// UnknownTopLevelKeys returns the top-level mapping keys of a hooks document
// that name no event, so an inspector can tell a publisher that a stanza will
// be silently ignored rather than run. The common mistake it catches is
// wrapping the events in a `hooks:` block, which is the settings.yaml shape:
// a plugin's hooks.yaml puts the events at the top level, so a `hooks:` wrapper
// parses to nothing and the plugin contributes no hooks at all.
//
// A document that is not a mapping (empty, a comment, a list) has no keys and
// returns none; malformed YAML is left for ParseConfig to report.
func UnknownTopLevelKeys(data []byte) []string {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil
	}
	known := make(map[string]bool, len(EventNames()))
	for _, e := range EventNames() {
		known[e] = true
	}
	// The YAML tags are the snake_case event keys, not the Go field names.
	for _, k := range []string{
		"session_start", "session_end", "user_prompt_submit", "pre_tool_use",
		"post_tool_use", "post_tool_use_failure", "notification", "pre_compact",
		"post_compact", "subagent_start", "subagent_stop", "stop", "stop_failure",
		"worktree_create", "worktree_remove", "cwd_changed",
	} {
		known[k] = true
	}
	var unknown []string
	content := doc.Content[0].Content
	for i := 0; i+1 < len(content); i += 2 {
		key := content[i].Value
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	return unknown
}
