package db

import (
	"context"

	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// The queries below feed `buildmax-server storage verify`, which walks every row
// that names a stored object. Each walks in keyset pages over a unique public
// handle rather than by offset, so a page costs one index range scan however
// deep the walk is, and the caller holds one page at a time. The cursor is the
// last handle of the previous page (entity-identity.md §9.3); "" starts the walk.

// ListLiveArtifactsAfter returns live artifacts ordered by id, after afterID.
// A tombstoned artifact is excluded: it is explicitly no longer served, and its
// bytes may already have been purged by retention.
func (s *Store) ListLiveArtifactsAfter(ctx context.Context, afterID string, limit int) ([]coreartifact.Artifact, error) {
	limit, _ = clampPage(limit, 0)
	var rows []artifactReadRow
	err := s.artifactSelect(ctx).
		Where("artifact.deleted_at IS NULL AND artifact.public_id > ?", afterID).
		Order("artifact.public_id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return toArtifacts(rows), nil
}

// ListWorkspaceCheckpointPayloadsAfter returns each checkpoint's payload claim
// ordered by checkpoint id, after afterID.
func (s *Store) ListWorkspaceCheckpointPayloadsAfter(ctx context.Context, afterID string, limit int) ([]coretask.CheckpointPayloadRef, error) {
	limit, _ = clampPage(limit, 0)
	var rows []struct {
		CheckpointID  string
		SpaceID       string
		StorageKey    string
		PayloadSHA256 string
		SizeBytes     int64
	}
	err := s.db.WithContext(ctx).
		Model(&workspaceCheckpointRow{}).
		Select("workspace_checkpoint.public_id AS checkpoint_id, sp.public_id AS space_id, "+
			"workspace_checkpoint.storage_key AS storage_key, workspace_checkpoint.payload_sha256 AS payload_sha256, "+
			"workspace_checkpoint.size_bytes AS size_bytes").
		Joins("INNER JOIN space sp ON sp.id = workspace_checkpoint.space_id").
		Where("workspace_checkpoint.public_id > ?", afterID).
		Order("workspace_checkpoint.public_id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coretask.CheckpointPayloadRef, len(rows))
	for i, r := range rows {
		out[i] = coretask.CheckpointPayloadRef(r)
	}
	return out, nil
}

// ListTaskRunTracesAfter returns every run that points at a trace, ordered by
// run id, after afterRunID. A run whose pointer retention already cleared is
// not a reference and is not returned.
func (s *Store) ListTaskRunTracesAfter(ctx context.Context, afterRunID string, limit int) ([]coretask.RunTraceRef, error) {
	limit, _ = clampPage(limit, 0)
	var rows []struct {
		SpaceID   string
		TaskID    string
		RunID     string
		TracePath string
	}
	err := s.db.WithContext(ctx).
		Model(&taskRunRow{}).
		Select("sp.public_id AS space_id, t.public_id AS task_id, task_run.public_id AS run_id, task_run.trace_path AS trace_path").
		Joins("INNER JOIN task t ON t.id = task_run.task_id").
		Joins("INNER JOIN space sp ON sp.id = t.space_id").
		Where("task_run.trace_path IS NOT NULL AND task_run.trace_path <> ''").
		Where("task_run.public_id > ?", afterRunID).
		Order("task_run.public_id ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coretask.RunTraceRef, len(rows))
	for i, r := range rows {
		out[i] = coretask.RunTraceRef{SpaceID: r.SpaceID, TaskID: r.TaskID, TaskRunID: r.RunID, TracePath: r.TracePath}
	}
	return out, nil
}

// ListPluginReleasesAfter returns every release, yanked ones included, ordered
// by (plugin name, version) after the given pair. A yanked release is still
// installable by an exact pin, so its bytes are still owed.
//
// Only the storage fields are read: the check needs no publisher or report,
// and joining the publisher would hide a release whose account row is gone.
func (s *Store) ListPluginReleasesAfter(ctx context.Context, afterName, afterVersion string, limit int) ([]coreplugin.Release, error) {
	limit, _ = clampPage(limit, 0)
	var rows []pluginReleaseRow
	err := s.db.WithContext(ctx).
		Select("plugin_name", "version", "digest", "object_key", "size_bytes").
		Where("plugin_name > ? OR (plugin_name = ? AND version > ?)", afterName, afterName, afterVersion).
		Order("plugin_name ASC, version ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coreplugin.Release, len(rows))
	for i, r := range rows {
		out[i] = coreplugin.Release{
			PluginName: r.PluginName,
			Version:    r.Version,
			Digest:     r.Digest,
			ObjectKey:  r.ObjectKey,
			SizeBytes:  r.SizeBytes,
		}
	}
	return out, nil
}
