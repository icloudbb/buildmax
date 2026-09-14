package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	"github.com/icloudbb/buildmax/internal/util"
)

// auditEventRow is the governance evidence table. It records that an action
// happened and who performed it — never what was said, generated, or run.
//
// Nothing updates a row here. A record that can be edited is not evidence. The
// one delete is PruneAuditEvents, which expires rows by age under the
// deployment's retention window and cannot be aimed at a particular record; the
// sweep that calls it writes down what it removed.
type auditEventRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_audit_event_public_id;not null"`

	// SpaceID is NULL for actions with no space, such as a login -- a pointer
	// rather than a zero, because zero is a row key someone owns. The composite
	// index leads with it because reading is always "this space, newest first".
	SpaceID   *uint64   `gorm:"column:space_id;index:idx_audit_space_time,priority:1"`
	CreatedAt time.Time `gorm:"not null;index:idx_audit_space_time,priority:2"`

	// The actor and the target stay opaque handles. Their type is in a column
	// beside them and admits values that are not rows at all -- an operator, a
	// worker, a permission name -- so one numeric reference cannot name them.
	ActorType string `gorm:"type:varchar(16);not null"`
	ActorID   string `gorm:"type:varchar(64);not null;index"`

	Action     string `gorm:"type:varchar(64);not null;index"`
	TargetType string `gorm:"type:varchar(32)"`
	TargetID   string `gorm:"type:varchar(64)"`

	// TaskRunID is the run an action was taken on behalf of, empty for the
	// actions a person takes directly. It is an opaque public handle like the
	// actor and target, not a foreign key, so an investigation reaches the run's
	// trace and ledger by it without this table joining to the run plane. The
	// index is what lets the pivot run the other way — every event a run caused.
	TaskRunID string `gorm:"column:task_run_id;type:varchar(20);index"`

	Detail string `gorm:"type:varchar(255)"`
}

func (auditEventRow) TableName() string { return "audit_event" }

// auditEventReadRow is the row plus its space's handle. A pointer field is one
// a LEFT JOIN may leave NULL.
type auditEventReadRow struct {
	Row           auditEventRow `gorm:"embedded"`
	SpacePublicID *string       `gorm:"column:space_public_id"`
}

func (s *Store) auditSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&auditEventRow{}).
		Select("audit_event.*, t.public_id AS space_public_id").
		Joins("LEFT JOIN space t ON t.id = audit_event.space_id")
}

func toAuditEvent(row *auditEventReadRow) *coreaudit.Event {
	if row == nil {
		return nil
	}
	return &coreaudit.Event{
		ID:         row.Row.PublicID,
		SpaceID:    derefPublicID(row.SpacePublicID),
		ActorType:  row.Row.ActorType,
		ActorID:    row.Row.ActorID,
		Action:     row.Row.Action,
		TargetType: row.Row.TargetType,
		TargetID:   row.Row.TargetID,
		TaskRunID:  row.Row.TaskRunID,
		Detail:     row.Row.Detail,
		CreatedAt:  row.Row.CreatedAt,
	}
}

// RecordAuditEvent appends one event.
func (s *Store) RecordAuditEvent(ctx context.Context, in coreaudit.Event) error {
	return writeAuditEvent(ctx, s.db.WithContext(ctx), in)
}

// writeAuditEvent inserts one audit row on the given handle, which may be a
// transaction. It is what lets a state change and the record of it commit
// together: an event written on the same tx as the change it describes cannot
// lag behind or be lost while the change stands. RecordAuditEvent is the
// best-effort, own-connection form; a caller that needs atomicity passes its tx.
func writeAuditEvent(ctx context.Context, tx *gorm.DB, in coreaudit.Event) error {
	publicID, err := util.NewPublicID()
	if err != nil {
		return err
	}
	// An event with no space is a login or an account action; it is still
	// evidence, so an unresolvable space is not a reason to lose the record.
	var spaceKey *uint64
	if in.SpaceID != "" {
		key, err := lookupKey(ctx, tx, "space", in.SpaceID)
		if err != nil && !errors.Is(err, apierr.ErrNotFound) {
			return err
		}
		if err == nil {
			spaceKey = &key
		}
	}
	// An event that already names a run keeps that id; otherwise it inherits the
	// run the request is serving, so a downstream write need not thread the id by
	// hand to be reachable from the run.
	taskRunID := in.TaskRunID
	if taskRunID == "" {
		taskRunID = coreaudit.RunFromContext(ctx)
	}
	row := auditEventRow{
		PublicID:   publicID,
		SpaceID:    spaceKey,
		ActorType:  in.ActorType,
		ActorID:    in.ActorID,
		Action:     in.Action,
		TargetType: in.TargetType,
		TargetID:   in.TargetID,
		TaskRunID:  taskRunID,
		Detail:     truncateDetail(in.Detail),
		CreatedAt:  time.Now().UTC(),
	}
	return tx.Create(&row).Error
}

// truncateDetail bounds the one free-text column. Detail is meant for a role
// name or a model name; bounding it means a caller that passes something
// larger loses the tail rather than failing the write and, with it, the record
// that the action happened.
func truncateDetail(s string) string {
	const max = 255
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// ListAuditEvents returns a space's events, newest first, with the total count.
func (s *Store) ListAuditEvents(ctx context.Context, spaceID string, limit, offset int) ([]coreaudit.Event, int, error) {
	limit, offset = clampPage(limit, offset)
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&auditEventRow{}).Where("space_id = ?", spaceKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []auditEventReadRow
	if err := s.auditSelect(ctx).Where("audit_event.space_id = ?", spaceKey).
		Order("audit_event.created_at DESC, audit_event.id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]coreaudit.Event, 0, len(rows))
	for i := range rows {
		out = append(out, *toAuditEvent(&rows[i]))
	}
	return out, int(total), nil
}

// SearchAuditEvents returns events across every space, newest first.
//
// The composite index leads with space_id, so a space-filtered search uses it and
// an unfiltered one is an ordered scan of a table that only grows by
// deliberate action. If that stops being true the answer is a second index on
// created_at, not a smaller retention — losing evidence to make a query fast is
// the wrong trade.
func (s *Store) SearchAuditEvents(ctx context.Context, filter coreaudit.Filter, limit, offset int) ([]coreaudit.Event, int, error) {
	limit, offset = clampPage(limit, offset)
	spaceKey, err := s.auditFilterSpaceKey(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := applyAuditFilter(s.db.WithContext(ctx).Model(&auditEventRow{}), "", filter, spaceKey).
		Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []auditEventReadRow
	if err := applyAuditFilter(s.auditSelect(ctx), "audit_event.", filter, spaceKey).
		Order("audit_event.created_at DESC, audit_event.id DESC").Limit(limit).Offset(offset).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]coreaudit.Event, 0, len(rows))
	for i := range rows {
		out = append(out, *toAuditEvent(&rows[i]))
	}
	return out, int(total), nil
}

// auditFilterSpaceKey resolves the filter's space once. A filter naming a space
// that does not exist matches nothing, which is reported as an empty page
// rather than an error: a search is allowed to find nothing.
func (s *Store) auditFilterSpaceKey(ctx context.Context, filter coreaudit.Filter) (*uint64, error) {
	if filter.WithoutSpace || filter.SpaceID == "" {
		return nil, nil
	}
	key, err := lookupKey(ctx, s.db, "space", filter.SpaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// applyAuditFilter narrows a query to the events a filter names. It is shared
// by the paged search and the export so the two cannot drift into answering
// different questions from the same parameters.
func applyAuditFilter(q *gorm.DB, col string, filter coreaudit.Filter, spaceKey *uint64) *gorm.DB {
	switch {
	case filter.WithoutSpace:
		q = q.Where(col + "space_id IS NULL")
	case spaceKey != nil:
		q = q.Where(col+"space_id = ?", *spaceKey)
	}
	if filter.ActorID != "" {
		q = q.Where(col+"actor_id = ?", filter.ActorID)
	}
	if filter.Action != "" {
		q = q.Where(col+"action = ?", filter.Action)
	}
	if filter.TaskRunID != "" {
		q = q.Where(col+"task_run_id = ?", filter.TaskRunID)
	}
	if !filter.Since.IsZero() {
		q = q.Where(col+"created_at >= ?", filter.Since)
	}
	if !filter.Until.IsZero() {
		q = q.Where(col+"created_at < ?", filter.Until)
	}
	return q
}

// applyAuditCursor continues a walk from where the last page stopped.
//
// The comparison is on the pair, not on the timestamp: created_at has
// one-second resolution, so several events can share it, and a `created_at <`
// bound alone would drop the ones that tied with the last row of the previous
// page.
//
// The tie-break is the row key, which no caller may hold, so the cursor names
// the event by its public handle and the key is resolved in a subquery. That
// keeps the translation where every other one lives -- inside this package --
// and costs one indexed lookup per page rather than a round trip per page.
func (s *Store) applyAuditCursor(ctx context.Context, q *gorm.DB, after coreaudit.Cursor) *gorm.DB {
	if after.Zero() {
		return q
	}
	id, ok := util.CanonicalPublicID(after.ID)
	if !ok {
		return q
	}
	key := s.db.WithContext(ctx).Model(&auditEventRow{}).Select("id").Where("public_id = ?", id)
	return q.Where("audit_event.created_at < ? OR (audit_event.created_at = ? AND audit_event.id < (?))",
		after.CreatedAt, after.CreatedAt, key)
}

func auditPageSize(limit int) int {
	if limit <= 0 {
		return 200
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}

func auditRowsToEvents(rows []auditEventReadRow) []coreaudit.Event {
	out := make([]coreaudit.Event, 0, len(rows))
	for i := range rows {
		out = append(out, *toAuditEvent(&rows[i]))
	}
	return out
}

// ExportSpaceAuditEvents returns one page of a space's events, newest first,
// continuing from after.
func (s *Store) ExportSpaceAuditEvents(ctx context.Context, spaceID string, after coreaudit.Cursor, limit int) ([]coreaudit.Event, error) {
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	q := s.auditSelect(ctx).Where("audit_event.space_id = ?", spaceKey)
	q = s.applyAuditCursor(ctx, q, after)

	var rows []auditEventReadRow
	if err := q.Order("audit_event.created_at DESC, audit_event.id DESC").Limit(auditPageSize(limit)).Find(&rows).Error; err != nil {
		return nil, err
	}
	return auditRowsToEvents(rows), nil
}

// ExportAuditEvents returns one page of events across every space, newest first,
// continuing from after.
func (s *Store) ExportAuditEvents(ctx context.Context, filter coreaudit.Filter, after coreaudit.Cursor, limit int) ([]coreaudit.Event, error) {
	spaceKey, err := s.auditFilterSpaceKey(ctx, filter)
	if err != nil {
		return nil, err
	}
	q := applyAuditFilter(s.auditSelect(ctx), "audit_event.", filter, spaceKey)
	q = s.applyAuditCursor(ctx, q, after)

	var rows []auditEventReadRow
	if err := q.Order("audit_event.created_at DESC, audit_event.id DESC").Limit(auditPageSize(limit)).Find(&rows).Error; err != nil {
		return nil, err
	}
	return auditRowsToEvents(rows), nil
}

// PruneAuditEvents deletes events recorded before the cutoff, at most limit of
// them, and returns how many went.
//
// This is the only delete anywhere near this table, and it is a retention
// policy rather than an edit: it removes rows by age and cannot be pointed at a
// particular record. The sweep that calls it records what it removed, so a
// trail that starts partway through says why it does — see
// coreaudit.EventsPruned.
func (s *Store) PruneAuditEvents(ctx context.Context, before time.Time, limit int) (int64, error) {
	if before.IsZero() {
		return 0, nil
	}
	if limit <= 0 {
		limit = 1000
	}
	// Select the ids first, then delete those. A bounded DELETE is not portable
	// SQL, and going through the primary key means the batch this sweep removes
	// is exactly the batch it chose — an unbounded predicate delete would be at
	// the mercy of whatever the driver did with the limit.
	var ids []uint
	if err := s.db.WithContext(ctx).Model(&auditEventRow{}).
		Where("created_at < ?", before).
		Order("created_at ASC, id ASC").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	res := s.db.WithContext(ctx).Where("id IN ?", ids).Delete(&auditEventRow{})
	return res.RowsAffected, res.Error
}

// OldestAuditEventAt returns the timestamp of the oldest event, or zero when
// there are none.
func (s *Store) OldestAuditEventAt(ctx context.Context) (time.Time, error) {
	var oldest []time.Time
	if err := s.db.WithContext(ctx).Model(&auditEventRow{}).
		Order("created_at ASC, id ASC").
		Limit(1).
		Pluck("created_at", &oldest).Error; err != nil {
		return time.Time{}, err
	}
	if len(oldest) == 0 {
		return time.Time{}, nil
	}
	return oldest[0], nil
}
