package db

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"

	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm"
)

type agentRow struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID     string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_agent_public_id;not null"`
	UserID       uint64 `gorm:"column:user_id;not null;index"`
	SpaceID      uint64 `gorm:"column:space_id;index"`
	Name         string `gorm:"type:varchar(255);not null"`
	Description  string `gorm:"type:text"`
	Instructions string `gorm:"type:text"`
	// Model is the catalog model name this agent's runs call, validated by
	// internal/service/agent before a write. Empty means the deployment
	// default. A plain string like the sandbox tiers: this row does not
	// depend on the gateway.
	Model string `gorm:"column:model;type:varchar(255)"`
	// Plugins is a JSON array of catalog names. A JSON column rather than a
	// join table because nothing queries inside it: the selection is written
	// and read whole, and "which agents name this plugin" is a scan of a
	// space's agents, which is a small set.
	Plugins string `gorm:"type:text"`
	// SandboxNetworkTier and SandboxFilesystemTier hold config.SandboxNetworkTier
	// / config.SandboxFilesystemTier values validated by internal/service/agent
	// before a write. Kept as plain strings here, the same way Plugins is a
	// JSON string: this row does not depend on the config package.
	SandboxNetworkTier    string `gorm:"column:sandbox_network_tier;type:varchar(64)"`
	SandboxFilesystemTier string `gorm:"column:sandbox_filesystem_tier;type:varchar(64)"`
	// SecretConsumption is a JSON object describing which Space Secrets this
	// agent consumes and how, written and read whole like Plugins.
	SecretConsumption string     `gorm:"column:secret_consumption;type:text"`
	Revision          int        `gorm:"column:revision;not null;default:1"`
	DeletedAt         *time.Time `gorm:"column:deleted_at;index"`
	CreatedAt         time.Time  `gorm:"autoCreateTime"`
}

func (agentRow) TableName() string { return "agent" }

// agentReadRow is the row plus the handles its references resolve to. A
// pointer field is one a LEFT JOIN may leave NULL.
type agentReadRow struct {
	Row           agentRow `gorm:"embedded"`
	UserPublicID  string   `gorm:"column:user_public_id"`
	SpacePublicID *string  `gorm:"column:space_public_id"`
}

func (s *Store) agentSelect(ctx context.Context) *gorm.DB {
	return agentSelectTx(s.db.WithContext(ctx))
}

func agentSelectTx(tx *gorm.DB) *gorm.DB {
	return tx.Model(&agentRow{}).
		Select("agent.*, u.public_id AS user_public_id, t.public_id AS space_public_id").
		Joins("INNER JOIN `user` u ON u.id = agent.user_id").
		Joins("LEFT JOIN space t ON t.id = agent.space_id")
}

// agentRevisionRow is one recorded version of an agent definition. Rows are
// appended, never updated or deleted, and they outlive the agent: deleting an
// agent leaves its history in place.
type agentRevisionRow struct {
	ID           uint64 `gorm:"primaryKey;autoIncrement"`
	AgentID      uint64 `gorm:"column:agent_id;not null;index:idx_agent_revision,unique,priority:1"`
	Revision     int    `gorm:"column:revision;not null;index:idx_agent_revision,unique,priority:2"`
	Name         string `gorm:"type:varchar(255);not null"`
	Description  string `gorm:"type:text"`
	Instructions string `gorm:"type:text"`
	// Model mirrors agentRow's column of the same name, versioned with the
	// rest of the revision.
	Model   string `gorm:"column:model;type:varchar(255)"`
	Plugins string `gorm:"type:text"`
	// SandboxNetworkTier and SandboxFilesystemTier mirror agentRow's columns
	// of the same name, versioned with the rest of the revision.
	SandboxNetworkTier    string    `gorm:"column:sandbox_network_tier;type:varchar(64)"`
	SandboxFilesystemTier string    `gorm:"column:sandbox_filesystem_tier;type:varchar(64)"`
	SecretConsumption     string    `gorm:"column:secret_consumption;type:text"`
	CreatedBy             uint64    `gorm:"column:created_by;not null"`
	CreatedAt             time.Time `gorm:"autoCreateTime"`
}

func (agentRevisionRow) TableName() string { return "agent_revision" }

// encodePluginSelection and decodePluginSelection are the column's two ends.
//
// An empty selection is stored as the empty string rather than "[]" so a row
// written before the column existed and one saved with no plugins read the
// same. A document that will not decode costs the selection, not the agent:
// the definition is still what somebody wrote.
func encodePluginSelection(names []string) string {
	if len(names) == 0 {
		return ""
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func decodePluginSelection(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func encodeSecretConsumption(c agentdef.SecretConsumption) string {
	if c.IsEmpty() {
		return ""
	}
	encoded, err := json.Marshal(c.Canonical())
	if err != nil {
		return ""
	}
	return string(encoded)
}

func decodeSecretConsumption(raw string) agentdef.SecretConsumption {
	if raw == "" {
		return agentdef.SecretConsumption{}
	}
	var c agentdef.SecretConsumption
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return agentdef.SecretConsumption{}
	}
	return c
}

// agentRevisionReadRow is the row plus the handles its references resolve to.
// The revision itself has none: it is addressed by its agent plus its number.
type agentRevisionReadRow struct {
	Row               agentRevisionRow `gorm:"embedded"`
	AgentPublicID     string           `gorm:"column:agent_public_id"`
	CreatedByPublicID string           `gorm:"column:created_by_public_id"`
}

func (s *Store) agentRevisionSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&agentRevisionRow{}).
		Select("agent_revision.*, a.public_id AS agent_public_id, cb.public_id AS created_by_public_id").
		Joins("INNER JOIN agent a ON a.id = agent_revision.agent_id").
		Joins("INNER JOIN `user` cb ON cb.id = agent_revision.created_by")
}

func toAgent(row *agentReadRow) *agentdef.Agent {
	if row == nil {
		return nil
	}
	return &agentdef.Agent{
		ID:                    row.Row.PublicID,
		UserID:                row.UserPublicID,
		SpaceID:               derefPublicID(row.SpacePublicID),
		Name:                  row.Row.Name,
		Description:           row.Row.Description,
		Instructions:          row.Row.Instructions,
		Model:                 row.Row.Model,
		Plugins:               decodePluginSelection(row.Row.Plugins),
		SandboxNetworkTier:    row.Row.SandboxNetworkTier,
		SandboxFilesystemTier: row.Row.SandboxFilesystemTier,
		SecretConsumption:     decodeSecretConsumption(row.Row.SecretConsumption),
		Revision:              row.Row.Revision,
		DeletedAt:             row.Row.DeletedAt,
		CreatedAt:             row.Row.CreatedAt,
	}
}

func toAgentRevision(row *agentRevisionReadRow) *agentdef.Revision {
	if row == nil {
		return nil
	}
	return &agentdef.Revision{
		AgentID:               row.AgentPublicID,
		Revision:              row.Row.Revision,
		Name:                  row.Row.Name,
		Description:           row.Row.Description,
		Instructions:          row.Row.Instructions,
		Model:                 row.Row.Model,
		Plugins:               decodePluginSelection(row.Row.Plugins),
		SandboxNetworkTier:    row.Row.SandboxNetworkTier,
		SandboxFilesystemTier: row.Row.SandboxFilesystemTier,
		SecretConsumption:     decodeSecretConsumption(row.Row.SecretConsumption),
		CreatedBy:             row.CreatedByPublicID,
		CreatedAt:             row.Row.CreatedAt,
	}
}

func toAgentRevisions(rows []agentRevisionReadRow) []agentdef.Revision {
	out := make([]agentdef.Revision, len(rows))
	for i := range rows {
		out[i] = *toAgentRevision(&rows[i])
	}
	return out
}

func toAgents(rows []agentReadRow) []agentdef.Agent {
	out := make([]agentdef.Agent, len(rows))
	for i := range rows {
		out[i] = *toAgent(&rows[i])
	}
	return out
}

// appendAgentRevision records a's current content as its revision. It runs in
// the same transaction as the write it describes, so history cannot drift from
// the row: the unique (agent_id, revision) index makes a concurrent second
// write fail rather than record two different definitions under one number.
func appendAgentRevision(ctx context.Context, tx *gorm.DB, agentKey uint64, a *agentdef.Agent, createdBy string) error {
	creator, err := lookupKey(ctx, tx, "user", createdBy)
	if err != nil {
		return err
	}
	return tx.Create(&agentRevisionRow{
		AgentID:               agentKey,
		Revision:              a.Revision,
		Name:                  a.Name,
		Description:           a.Description,
		Instructions:          a.Instructions,
		Model:                 a.Model,
		Plugins:               encodePluginSelection(a.Plugins),
		SandboxNetworkTier:    a.SandboxNetworkTier,
		SandboxFilesystemTier: a.SandboxFilesystemTier,
		SecretConsumption:     encodeSecretConsumption(a.SecretConsumption),
		CreatedBy:             creator,
		CreatedAt:             time.Now().UTC(),
	}).Error
}

// GetAgent returns the live agent by agent_id, or (nil, nil) when there is none.
// A deleted agent reads as not found here; see GetAgentIncludingDeleted.
func (s *Store) GetAgent(ctx context.Context, agentID string) (*agentdef.Agent, error) {
	id, ok := util.CanonicalPublicID(agentID)
	if !ok {
		return nil, nil
	}
	var a agentReadRow
	err := s.agentSelect(ctx).Where("agent.public_id = ? AND agent.deleted_at IS NULL", id).Take(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toAgent(&a), nil
}

// GetAgentIncludingDeleted returns the agent whether or not it was deleted. It
// answers "what did this record refer to", not "what may I use".
func (s *Store) GetAgentIncludingDeleted(ctx context.Context, agentID string) (*agentdef.Agent, error) {
	id, ok := util.CanonicalPublicID(agentID)
	if !ok {
		return nil, nil
	}
	var a agentReadRow
	err := s.agentSelect(ctx).Where("agent.public_id = ?", id).Take(&a).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toAgent(&a), nil
}

// ListAgentsByUser returns all agents for the given user_id, ordered by created_at ASC.
func (s *Store) ListAgentsByUser(ctx context.Context, userID string) ([]agentdef.Agent, error) {
	id, ok := util.CanonicalPublicID(userID)
	if !ok {
		return nil, nil
	}
	var list []agentReadRow
	err := s.agentSelect(ctx).Where("u.public_id = ? AND agent.deleted_at IS NULL", id).
		Order("agent.created_at ASC").Find(&list).Error
	return toAgents(list), err
}

// ListAgentsBySpace returns all agents for the given space_id, ordered by created_at ASC.
func (s *Store) ListAgentsBySpace(ctx context.Context, spaceID string) ([]agentdef.Agent, error) {
	id, ok := util.CanonicalPublicID(spaceID)
	if !ok {
		return nil, nil
	}
	var list []agentReadRow
	err := s.agentSelect(ctx).Where("t.public_id = ? AND agent.deleted_at IS NULL", id).
		Order("agent.created_at ASC").Find(&list).Error
	return toAgents(list), err
}

// CreateAgentInSpace inserts a new space-scoped agent and returns it.
//
// An empty SpaceID puts the agent in the user's personal space, which is what a
// non-space caller means by "my agent".
func (s *Store) CreateAgentInSpace(ctx context.Context, in agentdef.CreateInput) (*agentdef.Agent, error) {
	spaceID := in.SpaceID
	if spaceID == "" {
		resolved, err := s.personalSpaceIDForUser(ctx, in.UserID)
		if err != nil {
			return nil, err
		}
		spaceID = resolved
	}
	a := &agentdef.Agent{
		UserID:                in.UserID,
		SpaceID:               spaceID,
		Name:                  in.Def.Name,
		Description:           in.Def.Description,
		Instructions:          in.Def.Instructions,
		Model:                 in.Def.Model,
		Plugins:               in.Def.Plugins,
		SandboxNetworkTier:    in.Def.SandboxNetworkTier,
		SandboxFilesystemTier: in.Def.SandboxFilesystemTier,
		SecretConsumption:     in.Def.SecretConsumption,
		Revision:              1,
		CreatedAt:             time.Now().UTC(),
	}
	row := &agentRow{
		Name:                  a.Name,
		Description:           a.Description,
		Instructions:          a.Instructions,
		Model:                 a.Model,
		Plugins:               encodePluginSelection(a.Plugins),
		SandboxNetworkTier:    a.SandboxNetworkTier,
		SandboxFilesystemTier: a.SandboxFilesystemTier,
		SecretConsumption:     encodeSecretConsumption(a.SecretConsumption),
		Revision:              1,
		CreatedAt:             a.CreatedAt,
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		userKey, err := lookupKey(ctx, tx, "user", in.UserID)
		if err != nil {
			return err
		}
		row.UserID = userKey
		if spaceID != "" {
			spaceKey, err := lookupKey(ctx, tx, "space", spaceID)
			if err != nil {
				return err
			}
			row.SpaceID = spaceKey
		}
		if err := createWithPublicID(ctx, tx, "uq_agent_public_id",
			func(id string) { row.PublicID = id }, row); err != nil {
			return err
		}
		return appendAgentRevision(ctx, tx, row.ID, a, in.UserID)
	})
	if err != nil {
		return nil, err
	}
	a.ID = row.PublicID
	return a, nil
}

// UpdateAgentInSpace updates a space-scoped agent. Returns (nil, nil) if not
// found or the space does not match.
func (s *Store) UpdateAgentInSpace(ctx context.Context, in agentdef.UpdateInput) (*agentdef.Agent, error) {
	a, err := s.GetAgent(ctx, in.AgentID)
	if err != nil || a == nil {
		return nil, err
	}
	if a.SpaceID != in.SpaceID {
		return nil, nil
	}
	return s.updateAgent(ctx, a, in.UpdatedBy, in.Def)
}

// updateAgent writes new content and records it as the next revision. An update
// that changes nothing is not a revision: a save with no edit should not add a
// row that a reader has to compare against its predecessor to dismiss.
func (s *Store) updateAgent(ctx context.Context, a *agentdef.Agent, updatedBy string, def agentdef.Definition) (*agentdef.Agent, error) {
	if a.Name == def.Name && a.Description == def.Description &&
		a.Instructions == def.Instructions && a.Model == def.Model && slices.Equal(a.Plugins, def.Plugins) &&
		a.SandboxNetworkTier == def.SandboxNetworkTier && a.SandboxFilesystemTier == def.SandboxFilesystemTier &&
		a.SecretConsumption.Equal(def.SecretConsumption) {
		return a, nil
	}
	updated := *a
	updated.Name = def.Name
	updated.Description = def.Description
	updated.Instructions = def.Instructions
	updated.Model = def.Model
	updated.Plugins = def.Plugins
	updated.SandboxNetworkTier = def.SandboxNetworkTier
	updated.SandboxFilesystemTier = def.SandboxFilesystemTier
	updated.SecretConsumption = def.SecretConsumption
	updated.Revision = nextRevision(a.Revision)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Addressed by its handle rather than by saving a row built from the
		// model: the model carries no row key, so Save would see a zero primary
		// key and insert a second agent instead of updating this one.
		agentKey, err := lookupKey(ctx, tx, "agent", updated.ID)
		if err != nil {
			return err
		}
		res := tx.Model(&agentRow{}).Where("id = ?", agentKey).Updates(map[string]any{
			"name":                    updated.Name,
			"description":             updated.Description,
			"instructions":            updated.Instructions,
			"model":                   updated.Model,
			"plugins":                 encodePluginSelection(updated.Plugins),
			"sandbox_network_tier":    updated.SandboxNetworkTier,
			"sandbox_filesystem_tier": updated.SandboxFilesystemTier,
			"secret_consumption":      encodeSecretConsumption(updated.SecretConsumption),
			"revision":                updated.Revision,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apierr.ErrNotFound
		}
		return appendAgentRevision(ctx, tx, agentKey, &updated, updatedBy)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

// ListAgentRevisions returns an agent's revisions, newest first.
func (s *Store) ListAgentRevisions(ctx context.Context, agentID string, limit, offset int) ([]agentdef.Revision, int, error) {
	limit, offset = capPage(limit, offset)
	agentKey, err := lookupKey(ctx, s.db, "agent", agentID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := listRevisions[agentRevisionReadRow](ctx, s.db, s.agentRevisionSelect(ctx),
		"agent_revision", "agent_id", agentKey, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	return toAgentRevisions(rows), total, nil
}

// GetAgentRevision returns one revision, or (nil, nil) when there is no such revision.
func (s *Store) GetAgentRevision(ctx context.Context, agentID string, revision int) (*agentdef.Revision, error) {
	agentKey, err := lookupKey(ctx, s.db, "agent", agentID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	row, err := getRevision[agentRevisionReadRow](s.agentRevisionSelect(ctx),
		"agent_revision", "agent_id", agentKey, revision)
	if err != nil || row == nil {
		return nil, err
	}
	return toAgentRevision(row), nil
}

// DeleteAgent marks the agent deleted if it exists and belongs to the user.
// Returns apierr.ErrNotFound if there is no such live agent for that user.
func (s *Store) DeleteAgent(ctx context.Context, agentID, userID string) error {
	a, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if a == nil || a.UserID != userID {
		return apierr.ErrNotFound
	}
	return s.markAgentDeleted(ctx, a.ID)
}

// DeleteAgentInSpace marks the agent deleted if it exists and belongs to the space.
func (s *Store) DeleteAgentInSpace(ctx context.Context, agentID, spaceID string) error {
	a, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if a == nil || a.SpaceID != spaceID {
		return apierr.ErrNotFound
	}
	return s.markAgentDeleted(ctx, a.ID)
}

// markAgentDeleted stamps deleted_at instead of removing the row.
//
// Tasks, workflow step runs, and revisions all name an agent by ID. Dropping
// the row turned every one of those into a dangling reference and broke any
// workflow run still in flight at its next step; keeping it costs one column
// and leaves the record readable.
func (s *Store) markAgentDeleted(ctx context.Context, agentID string) error {
	return s.db.WithContext(ctx).
		Model(&agentRow{}).
		Where("public_id = ? AND deleted_at IS NULL", canonicalPublicID(agentID)).
		Update("deleted_at", time.Now().UTC()).Error
}
