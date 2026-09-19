package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/util"
)

// llmCallRow is the managed call ledger. It stores accounting and diagnostic
// metadata only — no prompts, tool payloads, or generated content.
type llmCallRow struct {
	ID           uint    `gorm:"primaryKey;autoIncrement"`
	PublicID     string  `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_llm_call_public_id;not null"`
	ClientCallID *string `gorm:"type:varchar(128);uniqueIndex:idx_llm_call_client,priority:2"`

	// A call is attributed to a person, not a space: a foreground call belongs to
	// no space, and a run's space is reached through task_run_id. The composite
	// unique index leads with user_id, which is both the idempotency scope and
	// the column usage is grouped by.
	UserID    *uint64 `gorm:"column:user_id;uniqueIndex:idx_llm_call_client,priority:1"`
	TaskRunID *uint64 `gorm:"column:task_run_id;index"`

	// SessionID names a file under a run's BUILDMAX_HOME, not a row.
	Surface   string  `gorm:"type:varchar(32)"`
	SessionID *string `gorm:"type:varchar(64)"`
	TaskID    *uint64 `gorm:"column:task_id;index"`

	// Model and TargetID name a catalog entry that may have been renamed or
	// retired since, so neither becomes a reference. Model is what the caller
	// asked for; TargetID is what served it.
	Model         string `gorm:"type:varchar(128)"`
	TargetID      string `gorm:"type:varchar(64);not null"`
	ProviderType  string `gorm:"type:varchar(32);not null"`
	UpstreamModel string `gorm:"type:varchar(128);not null"`
	Streaming     bool   `gorm:"not null;default:false"`

	AcceptedAt        time.Time  `gorm:"not null;index"`
	UpstreamStartedAt *time.Time `gorm:""`
	FirstDeltaAt      *time.Time `gorm:""`
	CompletedAt       *time.Time `gorm:""`

	Status     string  `gorm:"type:varchar(16);not null;index"`
	ErrorClass *string `gorm:"type:varchar(64)"`
	Attempts   int     `gorm:"not null;default:0"`

	PromptTokens     *int   `gorm:""`
	CompletionTokens *int   `gorm:""`
	TotalTokens      *int   `gorm:""`
	CacheReadTokens  *int   `gorm:""`
	CacheWriteTokens *int   `gorm:""`
	UsageSource      string `gorm:"type:varchar(16)"`

	// The rates that applied when this call ran, in nano-currency-units per
	// million tokens. They are a snapshot rather than a reference to the
	// catalog: a model's price changes, and recomputing an old call from the
	// new rates would rewrite what a space already spent. An empty Currency
	// means the model was unpriced at the time, which is not the same fact as
	// a call that cost nothing.
	// Column names pinned for the same reason as on llm_model: the naming
	// strategy renders MTok as "m_tok".
	Currency              string `gorm:"type:varchar(8)"`
	RateInputPerMTok      *int64 `gorm:"column:rate_input_per_mtok"`
	RateCacheReadPerMTok  *int64 `gorm:"column:rate_cache_read_per_mtok"`
	RateCacheWritePerMTok *int64 `gorm:"column:rate_cache_write_per_mtok"`
	RateOutputPerMTok     *int64 `gorm:"column:rate_output_per_mtok"`
}

func (llmCallRow) TableName() string { return "llm_call" }

// llmCallReadRow is the row plus the handles its references resolve to. A
// pointer field is one a LEFT JOIN may leave NULL.
type llmCallReadRow struct {
	Row             llmCallRow `gorm:"embedded"`
	UserPublicID    *string    `gorm:"column:user_public_id"`
	TaskPublicID    *string    `gorm:"column:task_public_id"`
	TaskRunPublicID *string    `gorm:"column:task_run_public_id"`
}

func (s *Store) llmCallSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&llmCallRow{}).
		Select("llm_call.*, u.public_id AS user_public_id, " +
			"tk.public_id AS task_public_id, r.public_id AS task_run_public_id").
		Joins("LEFT JOIN `user` u ON u.id = llm_call.user_id").
		Joins("LEFT JOIN task tk ON tk.id = llm_call.task_id").
		Joins("LEFT JOIN task_run r ON r.id = llm_call.task_run_id")
}

func toLLMCall(row *llmCallReadRow) *coregw.Call {
	if row == nil {
		return nil
	}
	out := &coregw.Call{
		ID:                row.Row.PublicID,
		ClientCallID:      row.Row.ClientCallID,
		Surface:           row.Row.Surface,
		SessionID:         row.Row.SessionID,
		Model:             row.Row.Model,
		TargetID:          row.Row.TargetID,
		ProviderType:      row.Row.ProviderType,
		UpstreamModel:     row.Row.UpstreamModel,
		Streaming:         row.Row.Streaming,
		AcceptedAt:        row.Row.AcceptedAt,
		UpstreamStartedAt: row.Row.UpstreamStartedAt,
		FirstDeltaAt:      row.Row.FirstDeltaAt,
		CompletedAt:       row.Row.CompletedAt,
		Status:            row.Row.Status,
		ErrorClass:        row.Row.ErrorClass,
		Attempts:          row.Row.Attempts,
		PromptTokens:      row.Row.PromptTokens,
		CompletionTokens:  row.Row.CompletionTokens,
		TotalTokens:       row.Row.TotalTokens,
		CacheReadTokens:   row.Row.CacheReadTokens,
		CacheWriteTokens:  row.Row.CacheWriteTokens,
		UsageSource:       row.Row.UsageSource,

		Currency:              row.Row.Currency,
		RateInputPerMTok:      row.Row.RateInputPerMTok,
		RateCacheReadPerMTok:  row.Row.RateCacheReadPerMTok,
		RateCacheWritePerMTok: row.Row.RateCacheWritePerMTok,
		RateOutputPerMTok:     row.Row.RateOutputPerMTok,
	}
	if row.Row.UserID != nil {
		user := derefPublicID(row.UserPublicID)
		out.UserID = &user
	}
	if row.Row.TaskID != nil {
		task := derefPublicID(row.TaskPublicID)
		out.TaskID = &task
	}
	if row.Row.TaskRunID != nil {
		run := derefPublicID(row.TaskRunPublicID)
		out.TaskRunID = &run
	}
	return out
}

// llmCallValues carries every column that is a value rather than a reference.
//
// It is separate from reference resolution so the mapping stays testable
// without a database: a column added to the model and forgotten here is the
// failure this split exists to keep catching.
func llmCallValues(call *coregw.Call) *llmCallRow {
	if call == nil {
		return nil
	}
	return &llmCallRow{
		ClientCallID:      call.ClientCallID,
		Surface:           call.Surface,
		SessionID:         call.SessionID,
		Model:             call.Model,
		TargetID:          call.TargetID,
		ProviderType:      call.ProviderType,
		UpstreamModel:     call.UpstreamModel,
		Streaming:         call.Streaming,
		AcceptedAt:        call.AcceptedAt,
		UpstreamStartedAt: call.UpstreamStartedAt,
		FirstDeltaAt:      call.FirstDeltaAt,
		CompletedAt:       call.CompletedAt,
		Status:            call.Status,
		ErrorClass:        call.ErrorClass,
		Attempts:          call.Attempts,
		PromptTokens:      call.PromptTokens,
		CompletionTokens:  call.CompletionTokens,
		TotalTokens:       call.TotalTokens,
		CacheReadTokens:   call.CacheReadTokens,
		CacheWriteTokens:  call.CacheWriteTokens,
		UsageSource:       call.UsageSource,

		Currency:              call.Currency,
		RateInputPerMTok:      call.RateInputPerMTok,
		RateCacheReadPerMTok:  call.RateCacheReadPerMTok,
		RateCacheWritePerMTok: call.RateCacheWritePerMTok,
		RateOutputPerMTok:     call.RateOutputPerMTok,
	}
}

// toLLMCallRow resolves the call's references. It reports an error rather than
// dropping one: the ledger is an accounting record, and a call attributed to
// nothing is worse than a refused write.
func (s *Store) toLLMCallRow(ctx context.Context, call *coregw.Call) (*llmCallRow, error) {
	userKey, err := optionalKey(ctx, s.db, "user", call.UserID)
	if err != nil {
		return nil, err
	}
	taskKey, err := optionalKey(ctx, s.db, "task", call.TaskID)
	if err != nil {
		return nil, err
	}
	runKey, err := optionalKey(ctx, s.db, "task_run", call.TaskRunID)
	if err != nil {
		return nil, err
	}
	row := llmCallValues(call)
	row.UserID = userKey
	row.TaskID = taskKey
	row.TaskRunID = runKey
	return row, nil
}

// OpenLLMCall records an accepted call before the upstream request starts, so a
// call that never returns still leaves evidence that it was attempted.
func (s *Store) OpenLLMCall(ctx context.Context, call *coregw.Call) (*coregw.Call, error) {
	if call == nil {
		return nil, errors.New("llm call is required")
	}
	stored := *call
	if stored.AcceptedAt.IsZero() {
		stored.AcceptedAt = time.Now().UTC()
	}
	if stored.Status == "" {
		stored.Status = coregw.CallStatusAccepted
	}
	if stored.UsageSource == "" {
		stored.UsageSource = coregw.UsageSourceUnavailable
	}
	row, err := s.toLLMCallRow(ctx, &stored)
	if err != nil {
		return nil, err
	}
	if err := createWithPublicID(ctx, s.db, "uq_llm_call_public_id",
		func(id string) { row.PublicID = id }, row); err != nil {
		if isDuplicateKey(err) {
			return nil, coregw.ErrDuplicateCall
		}
		return nil, err
	}
	return toLLMCall(&llmCallReadRow{
		Row:             *row,
		UserPublicID:    optionalCanonicalPublicID(stored.UserID),
		TaskPublicID:    optionalCanonicalPublicID(stored.TaskID),
		TaskRunPublicID: optionalCanonicalPublicID(stored.TaskRunID),
	}), nil
}

// CompleteLLMCall writes the terminal outcome of an open call. Usage is left as
// recorded when the outcome carries none, so an unavailable count is never
// silently written as zero.
func (s *Store) CompleteLLMCall(ctx context.Context, llmCallID string, outcome coregw.CallOutcome) error {
	updates := map[string]any{
		"status":       outcome.Status,
		"error_class":  outcome.ErrorClass,
		"attempts":     outcome.Attempts,
		"completed_at": outcome.CompletedAt,
	}
	if outcome.UpstreamStartedAt != nil {
		updates["upstream_started_at"] = outcome.UpstreamStartedAt
	}
	if outcome.FirstDeltaAt != nil {
		updates["first_delta_at"] = outcome.FirstDeltaAt
	}
	if usage := outcome.Usage; usage != nil {
		source := usage.Source
		if source == "" {
			source = coregw.UsageSourceReported
		}
		updates["prompt_tokens"] = usage.PromptTokens
		updates["completion_tokens"] = usage.CompletionTokens
		updates["total_tokens"] = usage.TotalTokens
		updates["cache_read_tokens"] = usage.CacheReadTokens
		updates["cache_write_tokens"] = usage.CacheWriteTokens
		updates["usage_source"] = source
	}
	id, ok := util.CanonicalPublicID(llmCallID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Model(&llmCallRow{}).
		Where("public_id = ?", id).
		Updates(updates).Error
}

// GetLLMCall returns one call by ID, or (nil, nil) when not found.
func (s *Store) GetLLMCall(ctx context.Context, llmCallID string) (*coregw.Call, error) {
	id, ok := util.CanonicalPublicID(llmCallID)
	if !ok {
		return nil, nil
	}
	var row llmCallReadRow
	err := s.llmCallSelect(ctx).Where("llm_call.public_id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toLLMCall(&row), nil
}

// GetLLMCallByClientID returns one user's call by their idempotency key.
// The lookup is user-scoped: one caller's key can never resolve another's call.
func (s *Store) GetLLMCallByClientID(ctx context.Context, userID, clientCallID string) (*coregw.Call, error) {
	if userID == "" || clientCallID == "" {
		return nil, nil
	}
	userKey, err := lookupKey(ctx, s.db, "user", userID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var row llmCallReadRow
	err = s.llmCallSelect(ctx).
		Where("llm_call.user_id = ? AND llm_call.client_call_id = ?", userKey, clientCallID).
		Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toLLMCall(&row), nil
}

// ListLLMCallsByTaskRun returns one run's calls, oldest first.
//
// A run belongs to exactly one space, so authorizing the run authorizes every
// row this returns. The caller establishes that before asking; there is no space
// column here to filter on afterwards.
func (s *Store) ListLLMCallsByTaskRun(ctx context.Context, taskRunID string) ([]coregw.Call, error) {
	runKey, err := lookupKey(ctx, s.db, "task_run", taskRunID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rows []llmCallReadRow
	err = s.llmCallSelect(ctx).
		Where("llm_call.task_run_id = ?", runKey).
		Order("llm_call.accepted_at ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]coregw.Call, 0, len(rows))
	for i := range rows {
		out = append(out, *toLLMCall(&rows[i]))
	}
	return out, nil
}

// SearchLLMCalls returns the calls matching filter, newest first, and the total
// that match it before the page window is applied.
//
// The order is accepted_at DESC so the page window rides the accepted_at index
// and an administrator sees the most recent spend first — the opposite of the
// per-run read, where following a single run in the order it happened is the
// point.
func (s *Store) SearchLLMCalls(ctx context.Context, filter coregw.CallFilter, limit, offset int) ([]coregw.Call, int, error) {
	limit, offset = clampPage(limit, offset)
	var total int64
	count := applyLLMCallFilter(s.db.WithContext(ctx).Model(&llmCallRow{}).
		Joins("LEFT JOIN `user` u ON u.id = llm_call.user_id"), filter)
	if err := count.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []llmCallReadRow
	err := applyLLMCallFilter(s.llmCallSelect(ctx), filter).
		Order("llm_call.accepted_at DESC").
		Limit(limit).Offset(offset).
		Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	out := make([]coregw.Call, 0, len(rows))
	for i := range rows {
		out = append(out, *toLLMCall(&rows[i]))
	}
	return out, int(total), nil
}

// applyLLMCallFilter adds the filter's bounds to a query. It is shared by the
// count and the page so the two can never disagree on what matches.
//
// The user bound is on the joined public id rather than a resolved key: an
// unparseable id matches nothing rather than erroring, which keeps a mistyped
// filter a narrow answer instead of a failed request.
func applyLLMCallFilter(q *gorm.DB, filter coregw.CallFilter) *gorm.DB {
	if filter.UserID != "" {
		if id, ok := util.CanonicalPublicID(filter.UserID); ok {
			q = q.Where("u.public_id = ?", id)
		} else {
			q = q.Where("1 = 0")
		}
	}
	if filter.Model != "" {
		q = q.Where("llm_call.model = ?", filter.Model)
	}
	if filter.Status != "" {
		q = q.Where("llm_call.status = ?", filter.Status)
	}
	if filter.Surface != "" {
		q = q.Where("llm_call.surface = ?", filter.Surface)
	}
	if !filter.Since.IsZero() {
		q = q.Where("llm_call.accepted_at >= ?", filter.Since)
	}
	if !filter.Until.IsZero() {
		q = q.Where("llm_call.accepted_at <= ?", filter.Until)
	}
	return q
}
