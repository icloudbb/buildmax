package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
)

// issueCommentRow is one statement about an issue.
//
// It carries no space_id: a comment's space is its issue's space, and every
// handler already loads the issue to authorize. Denormalizing the
// authorization key would give it a second place to be wrong. This follows
// conversation_message, which resolves its space through its conversation.
type issueCommentRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_issue_comment_public_id;not null"`
	IssueID  uint64 `gorm:"column:issue_id;not null;index:idx_issue_comment_issue_created,priority:1"`
	// AuthorID stays an opaque handle under author_kind, and the two source
	// columns record where a comment came from rather than a live relation.
	AuthorKind      string     `gorm:"type:varchar(16);not null"`
	AuthorID        string     `gorm:"type:varchar(64);not null"`
	Body            string     `gorm:"type:text;not null"`
	SourceTaskID    *uint64    `gorm:"column:source_task_id"`
	SourceTaskRunID *uint64    `gorm:"column:source_task_run_id"`
	CreatedAt       time.Time  `gorm:"autoCreateTime;index:idx_issue_comment_issue_created,priority:2"`
	EditedAt        *time.Time `gorm:"column:edited_at"`
}

func (issueCommentRow) TableName() string { return "issue_comment" }

// issueCommentReadRow is the row plus the handles its references resolve to. A
// pointer field is one a LEFT JOIN may leave NULL.
type issueCommentReadRow struct {
	Row                issueCommentRow `gorm:"embedded"`
	IssuePublicID      string          `gorm:"column:issue_public_id"`
	SourceTaskPublicID *string         `gorm:"column:source_task_public_id"`
	SourceRunPublicID  *string         `gorm:"column:source_run_public_id"`
}

func (s *Store) issueCommentSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&issueCommentRow{}).
		Select("issue_comment.*, i.public_id AS issue_public_id, st.public_id AS source_task_public_id, " +
			"sr.public_id AS source_run_public_id").
		Joins("INNER JOIN issue i ON i.id = issue_comment.issue_id").
		Joins("LEFT JOIN task st ON st.id = issue_comment.source_task_id").
		Joins("LEFT JOIN task_run sr ON sr.id = issue_comment.source_task_run_id")
}

func toIssueComment(row *issueCommentReadRow) *coreissue.Comment {
	if row == nil {
		return nil
	}
	out := &coreissue.Comment{
		ID:         row.Row.PublicID,
		IssueID:    row.IssuePublicID,
		AuthorKind: row.Row.AuthorKind,
		AuthorID:   row.Row.AuthorID,
		Body:       row.Row.Body,
		CreatedAt:  row.Row.CreatedAt,
		EditedAt:   row.Row.EditedAt,
	}
	if row.Row.SourceTaskID != nil {
		task := derefPublicID(row.SourceTaskPublicID)
		out.SourceTaskID = &task
	}
	if row.Row.SourceTaskRunID != nil {
		run := derefPublicID(row.SourceRunPublicID)
		out.SourceTaskRunID = &run
	}
	return out
}

func toIssueComments(rows []issueCommentReadRow) []coreissue.Comment {
	out := make([]coreissue.Comment, len(rows))
	for i := range rows {
		out[i] = *toIssueComment(&rows[i])
	}
	return out
}

// CreateIssueComment appends a comment to an issue.
func (s *Store) CreateIssueComment(ctx context.Context, in coreissue.CreateCommentInput) (*coreissue.Comment, error) {
	issueKey, err := lookupKey(ctx, s.db, "issue", in.IssueID)
	if err != nil {
		return nil, err
	}
	row := &issueCommentRow{
		IssueID:    issueKey,
		AuthorKind: in.AuthorKind,
		AuthorID:   in.AuthorID,
		Body:       in.Body,
		CreatedAt:  time.Now().UTC(),
	}
	read := &issueCommentReadRow{IssuePublicID: canonicalPublicID(in.IssueID)}
	if in.SourceTaskID != nil && *in.SourceTaskID != "" {
		key, err := lookupKey(ctx, s.db, "task", *in.SourceTaskID)
		if err != nil {
			return nil, err
		}
		row.SourceTaskID = &key
		read.SourceTaskPublicID = optionalCanonicalPublicID(in.SourceTaskID)
	}
	if in.SourceTaskRunID != nil && *in.SourceTaskRunID != "" {
		key, err := lookupKey(ctx, s.db, "task_run", *in.SourceTaskRunID)
		if err != nil {
			return nil, err
		}
		row.SourceTaskRunID = &key
		read.SourceRunPublicID = optionalCanonicalPublicID(in.SourceTaskRunID)
	}
	if err := createWithPublicID(ctx, s.db, "uq_issue_comment_public_id",
		func(id string) { row.PublicID = id }, row); err != nil {
		return nil, err
	}
	read.Row = *row
	return toIssueComment(read), nil
}

// ListIssueComments returns an issue's comments oldest first.
//
// Ordering is created_at then the row key. A public ID is random rather than
// time-ordered, so it is never a sort key.
func (s *Store) ListIssueComments(ctx context.Context, issueID string, limit, offset int) ([]coreissue.Comment, int, error) {
	limit, offset = capPage(limit, offset)
	issueKey, err := lookupKey(ctx, s.db, "issue", issueID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&issueCommentRow{}).Where("issue_id = ?", issueKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []issueCommentReadRow
	q := s.issueCommentSelect(ctx).Where("issue_comment.issue_id = ?", issueKey).
		Order("issue_comment.created_at ASC, issue_comment.id ASC")
	if limit > 0 {
		q = q.Limit(limit).Offset(offset)
	}
	if err := q.Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toIssueComments(list), int(total), nil
}

// GetIssueComment returns the comment by issue_comment_id, or (nil, nil) if not found.
func (s *Store) GetIssueComment(ctx context.Context, commentID string) (*coreissue.Comment, error) {
	id, ok := util.CanonicalPublicID(commentID)
	if !ok {
		return nil, nil
	}
	var row issueCommentReadRow
	err := s.issueCommentSelect(ctx).Where("issue_comment.public_id = ?", id).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toIssueComment(&row), nil
}

// UpdateIssueComment replaces the body and stamps edited_at.
func (s *Store) UpdateIssueComment(ctx context.Context, commentID, body string) (*coreissue.Comment, error) {
	updates := map[string]interface{}{
		"body":      body,
		"edited_at": time.Now().UTC(),
	}
	id, ok := util.CanonicalPublicID(commentID)
	if !ok {
		return nil, nil
	}
	if err := s.db.WithContext(ctx).Model(&issueCommentRow{}).Where("public_id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetIssueComment(ctx, commentID)
}

// DeleteIssueComment removes the comment.
//
// The delete is hard: this schema has no soft-delete precedent, and adding
// deleted_at to one table would set a convention every other table has to
// answer to. See docs/contribute/architecture/data-model.md.
func (s *Store) DeleteIssueComment(ctx context.Context, commentID string) error {
	id, ok := util.CanonicalPublicID(commentID)
	if !ok {
		return nil
	}
	return s.db.WithContext(ctx).Where("public_id = ?", id).Delete(&issueCommentRow{}).Error
}

// CountIssueComments returns comment totals for the given issues in one grouped
// query. Issues with no comments are absent from the map.
func (s *Store) CountIssueComments(ctx context.Context, issueIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(issueIDs) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(issueIDs))
	for _, handle := range issueIDs {
		if id, ok := util.CanonicalPublicID(handle); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []struct {
		IssuePublicID string
		Total         int
	}
	if err := s.db.WithContext(ctx).Model(&issueCommentRow{}).
		Select("i.public_id AS issue_public_id, COUNT(*) AS total").
		Joins("INNER JOIN issue i ON i.id = issue_comment.issue_id").
		Where("i.public_id IN ?", ids).
		Group("i.public_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.IssuePublicID] = row.Total
	}
	return out, nil
}

// CountIssueCommentsBySourceTaskRun counts the comments a single task run
// authored, by source_task_run_id. A run that does not exist has written none,
// so an unknown handle is zero rather than an error — the budget check that
// calls this treats "no run" as "no comments spent".
func (s *Store) CountIssueCommentsBySourceTaskRun(ctx context.Context, taskRunID string) (int, error) {
	runKey, err := lookupKey(ctx, s.db, "task_run", taskRunID)
	if errors.Is(err, apierr.ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&issueCommentRow{}).
		Where("source_task_run_id = ?", runKey).Count(&total).Error; err != nil {
		return 0, err
	}
	return int(total), nil
}
