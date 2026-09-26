package task

import (
	"errors"
	"time"
)

// ErrCheckpointConflict is returned when a checkpoint already exists for a
// (source_task_run_id, kind) pair with different bytes. Finalization is
// idempotent for identical bytes and a conflict for different ones; it never
// rewrites an accepted checkpoint. See
// docs/design/task-workspace-checkpoints.md §8.
var ErrCheckpointConflict = errors.New("workspace checkpoint conflict")

// A workspace checkpoint is an immutable, complete representation of a Task's
// workspace at one boundary. See docs/design/task-workspace-checkpoints.md.
//
// The storage key is deliberately absent: it is infrastructure data that never
// reaches domain JSON, a worker-visible response, a log, or a trace. A recovery
// choice names a checkpoint by its public handle instead.
type WorkspaceCheckpoint struct {
	ID                string         `json:"id"`
	SpaceID           string         `json:"space_id"`
	TaskID            string         `json:"task_id"`
	SourceTaskRunID   string         `json:"source_task_run_id"`
	BaseCheckpointID  *string        `json:"base_checkpoint_id,omitempty"`
	Kind              CheckpointKind `json:"kind"`
	PayloadFormat     string         `json:"payload_format"`
	PayloadSHA256     string         `json:"payload_sha256"`
	SizeBytes         int64          `json:"size_bytes"`
	UncompressedBytes int64          `json:"uncompressed_bytes"`
	EntryCount        int64          `json:"entry_count"`
	CreatedAt         time.Time      `json:"created_at"`
}

// CheckpointKind is which boundary a checkpoint was captured at. A TaskRun has
// at most one of each kind.
type CheckpointKind string

const (
	// CheckpointKindSeed is the workspace the first run observed, captured
	// before any model or tool call.
	CheckpointKindSeed CheckpointKind = "seed"
	// CheckpointKindSuccessful is a workspace captured after a run quiesced and
	// succeeded. Only this kind can advance the Task head automatically.
	CheckpointKindSuccessful CheckpointKind = "successful"
	// CheckpointKindPartial is a workspace captured after cancellation,
	// interruption, or failure. It is recovery evidence and never becomes the
	// Task head implicitly.
	CheckpointKindPartial CheckpointKind = "partial"
)

// ValidCheckpointKind reports whether k is one of the three defined kinds.
func ValidCheckpointKind(k CheckpointKind) bool {
	switch k {
	case CheckpointKindSeed, CheckpointKindSuccessful, CheckpointKindPartial:
		return true
	default:
		return false
	}
}

// AdvancesWorkspaceHead reports whether committing a checkpoint of this kind
// advances the Task's recoverable head. A seed establishes the first head; a
// successful result advances an existing one; a partial never does.
func AdvancesWorkspaceHead(k CheckpointKind) bool {
	return k == CheckpointKindSeed || k == CheckpointKindSuccessful
}

// PayloadFormatTarZstV1 is the first checkpoint payload format: one
// Zstandard-compressed tar archive. The format column leaves room for a later
// chunked or manifest-backed representation without changing this domain model.
const PayloadFormatTarZstV1 = "tar.zst.v1"

// WorkspaceRestoreStatus records what happened when a run tried to restore its
// workspace base. A missing pointer alone cannot tell "not requested" from
// "attempted and failed", so the status is stored explicitly.
type WorkspaceRestoreStatus string

const (
	WorkspaceRestoreNotRequested WorkspaceRestoreStatus = "not_requested"
	WorkspaceRestorePending      WorkspaceRestoreStatus = "pending"
	WorkspaceRestoreRestored     WorkspaceRestoreStatus = "restored"
	WorkspaceRestoreFailed       WorkspaceRestoreStatus = "failed"
)

// WorkspaceCheckpointStatus records what happened when a run tried to capture
// and commit a result or partial checkpoint. A run can succeed while its
// checkpoint fails; the two facts are distinct.
type WorkspaceCheckpointStatus string

const (
	WorkspaceCheckpointNotRequested WorkspaceCheckpointStatus = "not_requested"
	WorkspaceCheckpointPending      WorkspaceCheckpointStatus = "pending"
	WorkspaceCheckpointCommitted    WorkspaceCheckpointStatus = "committed"
	WorkspaceCheckpointFailed       WorkspaceCheckpointStatus = "failed"
)

// PluginEnvironmentStatus records whether a run changed the Task's Plugin
// environment. "unchanged" is the common case: most runs derive their
// environment from the Agent revision and Space activation and install nothing.
type PluginEnvironmentStatus string

const (
	PluginEnvironmentUnchanged PluginEnvironmentStatus = "unchanged"
	PluginEnvironmentPending   PluginEnvironmentStatus = "pending"
	PluginEnvironmentCommitted PluginEnvironmentStatus = "committed"
	PluginEnvironmentFailed    PluginEnvironmentStatus = "failed"
)

// FinalizeCheckpointInput is the descriptor of one captured checkpoint payload:
// what it is, which run produced it, and where its immutable bytes already
// live. The bytes are written to the payload store first; this records the
// authoritative metadata pointer for them. Its authoritative implementation is
// the db Store's FinalizeWorkspaceCheckpoint; the workspace service validates
// this before delegating. See docs/design/task-workspace-checkpoints.md §8.
type FinalizeCheckpointInput struct {
	SpaceID          string
	TaskID           string
	SourceTaskRunID  string
	BaseCheckpointID *string
	Kind             CheckpointKind

	PayloadFormat     string
	PayloadSHA256     string
	StorageKey        string
	SizeBytes         int64
	UncompressedBytes int64
	EntryCount        int64
}

// CheckpointPayloadRef is one checkpoint row's claim on its payload object, as
// the storage reference check reads it. Unlike WorkspaceCheckpoint it carries
// the storage key, so it stays inside the server process: the check compares
// the key against the one the store derives, because the orphan sweep keeps
// only blobs whose key a row records.
type CheckpointPayloadRef struct {
	CheckpointID  string
	SpaceID       string
	StorageKey    string
	PayloadSHA256 string
	SizeBytes     int64
}
