package workflow

import (
	"errors"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

// ErrInvalidRunTransition and ErrInvalidNodeRunTransition are returned when a
// caller asks a run or node run to move between statuses that
// ValidRunStatusTransition / ValidNodeRunTransition do not allow. They name a
// programming error, not a lost race: a refused-but-valid transition (the row
// was no longer at the expected status) is reported as a false result, not an
// error.
var (
	ErrInvalidRunTransition     = errors.New("invalid workflow run status transition")
	ErrInvalidNodeRunTransition = errors.New("invalid workflow node run status transition")
)

// ErrRevisionConflict means the workflow advanced between the revision the
// service observed and the write it guarded on it: another edit already appended
// the next revision. The caller re-reads and retries from the current revision.
// It is the conflict every content edit, status transition, and restore returns
// so a stale write is refused rather than overwriting a newer definition or
// leaking the duplicate-key error the append would otherwise raise.
var ErrRevisionConflict = apierr.New(apierr.KindConflict, "workflow changed since it was read")
