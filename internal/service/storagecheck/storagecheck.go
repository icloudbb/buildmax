// Package storagecheck proves that every database record naming a stored
// object still resolves to it: the V19 check an operator runs after restoring a
// database and bucket pair (docs/design/verification-program.md).
//
// It is read-only by decision. A finding is reported with the record's handle
// and left alone: whether to restore the object, re-run the work, or tombstone
// the record is an operator's call that a checker cannot make safely.
package storagecheck

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// Kind is one class of record that names a stored object.
type Kind string

const (
	KindArtifact      Kind = "artifact"
	KindCheckpoint    Kind = "workspace_checkpoint"
	KindTrace         Kind = "trace"
	KindPluginRelease Kind = "plugin_release"
)

// Kinds is every kind in the order a check walks them.
func Kinds() []Kind {
	return []Kind{KindArtifact, KindCheckpoint, KindTrace, KindPluginRelease}
}

// Problem is what is wrong with one reference.
type Problem string

const (
	// ProblemMissing: the store has no object where the record points.
	ProblemMissing Problem = "missing"
	// ProblemMismatch: an object is there but is not the one recorded — its
	// size or digest differs, or its recorded key is not the one the store
	// derives.
	ProblemMismatch Problem = "mismatch"
	// ProblemUnreadable: the store answered with something other than the
	// object or its absence, so the check could not decide.
	ProblemUnreadable Problem = "unreadable"
)

// Finding is one reference that does not resolve.
type Finding struct {
	Kind Kind
	// ID is the handle an operator looks the record up by: the artifact,
	// checkpoint, or task run id, or <plugin>@<version>.
	ID      string
	SpaceID string
	Problem Problem
	Detail  string
}

// Counts is one kind's tally.
type Counts struct {
	Checked    int
	Missing    int
	Mismatched int
	Unreadable int
}

// Findings is how many references of the kind did not resolve.
func (c Counts) Findings() int { return c.Missing + c.Mismatched + c.Unreadable }

// Report is the tally of a whole check. Findings themselves are streamed to
// OnFinding rather than kept, so a check over millions of rows holds one page.
type Report map[Kind]Counts

// Findings is the total across kinds.
func (r Report) Findings() int {
	n := 0
	for _, c := range r {
		n += c.Findings()
	}
	return n
}

// Records pages through the rows that name stored objects. Each method returns
// the page after the given handle, in handle order, and an empty page at the end.
type Records interface {
	ListLiveArtifactsAfter(ctx context.Context, afterID string, limit int) ([]coreartifact.Artifact, error)
	ListWorkspaceCheckpointPayloadsAfter(ctx context.Context, afterID string, limit int) ([]coretask.CheckpointPayloadRef, error)
	ListTaskRunTracesAfter(ctx context.Context, afterRunID string, limit int) ([]coretask.RunTraceRef, error)
	ListPluginReleasesAfter(ctx context.Context, afterName, afterVersion string, limit int) ([]coreplugin.Release, error)
}

// ArtifactContent opens an artifact the way a download does, from the key the
// store derives for it. apierr.ErrNotFound means absent.
type ArtifactContent interface {
	OpenArtifact(ctx context.Context, ref coreartifact.Ref) (io.ReadCloser, error)
}

// KeyedObjects opens an object by its recorded key. apierr.ErrNotFound means
// absent. Plugin package storage has exactly this shape.
type KeyedObjects interface {
	Open(ctx context.Context, key string) (io.ReadCloser, int64, error)
}

// CheckpointObjects opens checkpoint payloads and derives the key a payload
// with a given digest lives at.
type CheckpointObjects interface {
	KeyedObjects
	Key(spaceID, sha256hex string) (string, error)
}

// TraceObjects opens a run's durable trace the way the trace route does.
type TraceObjects interface {
	OpenTrace(ctx context.Context, ref coretask.RunTraceRef) (io.ReadCloser, error)
}

// Checker walks every kind of reference and checks each against its store.
type Checker struct {
	Records     Records
	Artifacts   ArtifactContent
	Checkpoints CheckpointObjects
	Traces      TraceObjects
	Packages    KeyedObjects

	// Checksums reads every object whole and compares its size and SHA-256
	// with the record. Off, the check proves only that each object exists,
	// which costs one request per object instead of every byte.
	Checksums bool
	// PageSize bounds how many rows one query returns; 0 takes the store's
	// default.
	PageSize int

	// OnFinding receives each finding as it is found. OnProgress receives a
	// kind's running tally after each page and once when the kind is done.
	OnFinding  func(Finding)
	OnProgress func(kind Kind, counts Counts, done bool)
}

// target is one reference reduced to what verifying it needs.
type target struct {
	id, spaceID string
	// sha256 and size are what the record claims; "" and -1 when it keeps none.
	sha256 string
	size   int64
	// keyProblem is set when the record's own key disagrees with the store's
	// derivation, which is a finding before any object is opened.
	keyProblem string
	open       func(ctx context.Context) (io.ReadCloser, error)
}

// Run checks every kind and returns the tally. It fails only when the database
// cannot be read: an object that cannot be read is a finding, not an error, so
// one bad object does not hide the rest.
func (c *Checker) Run(ctx context.Context) (Report, error) {
	report := Report{}
	for _, kind := range Kinds() {
		counts, err := c.walk(ctx, kind)
		report[kind] = counts
		if err != nil {
			return report, fmt.Errorf("check %s references: %w", kind, err)
		}
	}
	return report, nil
}

func (c *Checker) walk(ctx context.Context, kind Kind) (Counts, error) {
	var counts Counts
	next := c.pager(kind)
	for {
		page, err := next(ctx)
		if err != nil {
			return counts, err
		}
		if len(page) == 0 {
			break
		}
		for _, t := range page {
			if err := ctx.Err(); err != nil {
				return counts, err
			}
			counts.Checked++
			problem, detail := c.verify(ctx, t)
			if problem == "" {
				continue
			}
			switch problem {
			case ProblemMissing:
				counts.Missing++
			case ProblemMismatch:
				counts.Mismatched++
			default:
				counts.Unreadable++
			}
			if c.OnFinding != nil {
				c.OnFinding(Finding{Kind: kind, ID: t.id, SpaceID: t.spaceID, Problem: problem, Detail: detail})
			}
		}
		c.progress(kind, counts, false)
	}
	c.progress(kind, counts, true)
	return counts, nil
}

func (c *Checker) progress(kind Kind, counts Counts, done bool) {
	if c.OnProgress != nil {
		c.OnProgress(kind, counts, done)
	}
}

// pager returns a function yielding successive pages of one kind, keeping the
// keyset cursor between calls.
func (c *Checker) pager(kind Kind) func(context.Context) ([]target, error) {
	limit := c.PageSize
	switch kind {
	case KindArtifact:
		after := ""
		return func(ctx context.Context) ([]target, error) {
			rows, err := c.Records.ListLiveArtifactsAfter(ctx, after, limit)
			if err != nil || len(rows) == 0 {
				return nil, err
			}
			after = rows[len(rows)-1].ID
			out := make([]target, len(rows))
			for i, a := range rows {
				ref := coreartifact.Ref{SpaceID: a.SpaceID, ArtifactID: a.ID}
				out[i] = target{
					id: a.ID, spaceID: a.SpaceID, sha256: a.SHA256, size: a.SizeBytes,
					open: func(ctx context.Context) (io.ReadCloser, error) { return c.Artifacts.OpenArtifact(ctx, ref) },
				}
			}
			return out, nil
		}
	case KindCheckpoint:
		after := ""
		return func(ctx context.Context) ([]target, error) {
			rows, err := c.Records.ListWorkspaceCheckpointPayloadsAfter(ctx, after, limit)
			if err != nil || len(rows) == 0 {
				return nil, err
			}
			after = rows[len(rows)-1].CheckpointID
			out := make([]target, len(rows))
			for i, cp := range rows {
				out[i] = target{
					id: cp.CheckpointID, spaceID: cp.SpaceID, sha256: cp.PayloadSHA256, size: cp.SizeBytes,
					keyProblem: c.checkpointKeyProblem(cp),
					open:       openKeyed(c.Checkpoints, cp.StorageKey),
				}
			}
			return out, nil
		}
	case KindTrace:
		after := ""
		return func(ctx context.Context) ([]target, error) {
			rows, err := c.Records.ListTaskRunTracesAfter(ctx, after, limit)
			if err != nil || len(rows) == 0 {
				return nil, err
			}
			after = rows[len(rows)-1].TaskRunID
			out := make([]target, len(rows))
			for i, r := range rows {
				// A trace row records no digest or size, so it is checked for
				// existence even in checksum mode.
				out[i] = target{
					id: r.TaskRunID, spaceID: r.SpaceID, size: -1,
					open: func(ctx context.Context) (io.ReadCloser, error) { return c.Traces.OpenTrace(ctx, r) },
				}
			}
			return out, nil
		}
	case KindPluginRelease:
		afterName, afterVersion := "", ""
		return func(ctx context.Context) ([]target, error) {
			rows, err := c.Records.ListPluginReleasesAfter(ctx, afterName, afterVersion, limit)
			if err != nil || len(rows) == 0 {
				return nil, err
			}
			last := rows[len(rows)-1]
			afterName, afterVersion = last.PluginName, last.Version
			out := make([]target, len(rows))
			for i, r := range rows {
				t := target{
					id: r.PluginName + "@" + r.Version, size: r.SizeBytes,
					open: openKeyed(c.Packages, r.ObjectKey),
				}
				hexDigest, ok := strings.CutPrefix(r.Digest, "sha256:")
				if ok {
					t.sha256 = hexDigest
				} else {
					t.keyProblem = fmt.Sprintf("recorded digest %q is not a labelled sha256", r.Digest)
				}
				out[i] = t
			}
			return out, nil
		}
	default:
		return func(context.Context) ([]target, error) { return nil, nil }
	}
}

// checkpointKeyProblem reports a checkpoint whose recorded key is not the one
// the store derives from its space and digest. The worker restores from the
// derived key while the orphan sweep keeps only recorded keys, so a
// disagreement means the payload is either unreachable or due for deletion.
func (c *Checker) checkpointKeyProblem(cp coretask.CheckpointPayloadRef) string {
	derived, err := c.Checkpoints.Key(cp.SpaceID, cp.PayloadSHA256)
	if err != nil {
		return fmt.Sprintf("recorded digest does not derive a storage key: %v", err)
	}
	if derived != cp.StorageKey {
		return fmt.Sprintf("recorded storage key %q is not the derived key %q", cp.StorageKey, derived)
	}
	return ""
}

func openKeyed(store KeyedObjects, key string) func(context.Context) (io.ReadCloser, error) {
	return func(ctx context.Context) (io.ReadCloser, error) {
		rc, _, err := store.Open(ctx, key)
		return rc, err
	}
}

// verify decides one reference. Existence is proved by opening the object; a
// checksum check then reads it through a hash, so memory stays at one buffer
// whatever the object's size.
func (c *Checker) verify(ctx context.Context, t target) (Problem, string) {
	if t.keyProblem != "" {
		return ProblemMismatch, t.keyProblem
	}
	rc, err := t.open(ctx)
	if err != nil {
		if errors.Is(err, apierr.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
			return ProblemMissing, "no object at the recorded location"
		}
		return ProblemUnreadable, err.Error()
	}
	defer func() { _ = rc.Close() }()
	if !c.Checksums || t.sha256 == "" {
		return "", ""
	}
	h := sha256.New()
	n, err := io.Copy(h, rc)
	if err != nil {
		return ProblemUnreadable, fmt.Sprintf("read failed after %d bytes: %v", n, err)
	}
	if t.size >= 0 && n != t.size {
		return ProblemMismatch, fmt.Sprintf("object is %d bytes, record says %d", n, t.size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != t.sha256 {
		return ProblemMismatch, fmt.Sprintf("object sha256 %s, record says %s", got, t.sha256)
	}
	return "", ""
}
