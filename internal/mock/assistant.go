package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
)

// MockAssistantStore is an in-memory coreassistant.Store. Tokens are kept as
// given; sealing is the real store's concern.
type MockAssistantStore struct {
	mu         sync.Mutex
	next       int
	assistants map[string]*coreassistant.Assistant
	deleted    map[string]bool
	Revisions  map[string][]coreassistant.Definition
	bindings   map[string]*coreassistant.Binding
	tokens     map[string]string
}

func (m *MockAssistantStore) init() {
	if m.assistants == nil {
		m.assistants = map[string]*coreassistant.Assistant{}
		m.deleted = map[string]bool{}
		m.Revisions = map[string][]coreassistant.Definition{}
		m.bindings = map[string]*coreassistant.Binding{}
		m.tokens = map[string]string{}
	}
}

func cloneDef(d coreassistant.Definition) coreassistant.Definition {
	b, _ := json.Marshal(d)
	var out coreassistant.Definition
	_ = json.Unmarshal(b, &out)
	return out
}

func (m *MockAssistantStore) CreateAssistant(_ context.Context, spaceID, createdBy, sponsorUserID string, def coreassistant.Definition) (*coreassistant.Assistant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	m.next++
	now := time.Now().UTC()
	a := &coreassistant.Assistant{
		ID: fmt.Sprintf("asst%d", m.next), SpaceID: spaceID, Def: cloneDef(def), Revision: 1,
		State: coreassistant.StatePaused, SponsorUserID: sponsorUserID, CreatedBy: createdBy,
		CreatedAt: now, UpdatedAt: now,
	}
	m.assistants[a.ID] = a
	m.Revisions[a.ID] = []coreassistant.Definition{cloneDef(def)}
	out := *a
	return &out, nil
}

func (m *MockAssistantStore) GetAssistant(_ context.Context, id string) (*coreassistant.Assistant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	a := m.assistants[id]
	if a == nil || m.deleted[id] {
		return nil, nil
	}
	out := *a
	out.Def = cloneDef(a.Def)
	return &out, nil
}

func (m *MockAssistantStore) ListAssistantsBySpace(_ context.Context, spaceID string) ([]coreassistant.Assistant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	var out []coreassistant.Assistant
	for i := 1; i <= m.next; i++ {
		id := fmt.Sprintf("asst%d", i)
		if a := m.assistants[id]; a != nil && !m.deleted[id] && a.SpaceID == spaceID {
			out = append(out, *a)
		}
	}
	return out, nil
}

func (m *MockAssistantStore) UpdateAssistantDefinition(ctx context.Context, id, _ string, def coreassistant.Definition) (*coreassistant.Assistant, error) {
	m.mu.Lock()
	a := m.assistants[id]
	if a == nil || m.deleted[id] {
		m.mu.Unlock()
		return nil, coreassistant.ErrNotFound
	}
	before, _ := json.Marshal(a.Def)
	after, _ := json.Marshal(def)
	if string(before) != string(after) {
		a.Def = cloneDef(def)
		a.Revision++
		m.Revisions[id] = append(m.Revisions[id], cloneDef(def))
	}
	m.mu.Unlock()
	return m.GetAssistant(ctx, id)
}

func (m *MockAssistantStore) SetAssistantState(_ context.Context, id, state string) error {
	return m.mutate(id, func(a *coreassistant.Assistant) { a.State = state })
}

func (m *MockAssistantStore) SetAssistantSponsor(_ context.Context, id, sponsor string) error {
	return m.mutate(id, func(a *coreassistant.Assistant) { a.SponsorUserID = sponsor })
}

func (m *MockAssistantStore) mutate(id string, fn func(*coreassistant.Assistant)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	a := m.assistants[id]
	if a == nil || m.deleted[id] {
		return coreassistant.ErrNotFound
	}
	fn(a)
	return nil
}

func (m *MockAssistantStore) DeleteAssistant(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	if m.assistants[id] == nil || m.deleted[id] {
		return coreassistant.ErrNotFound
	}
	m.deleted[id] = true
	for bid, b := range m.bindings {
		if b.AssistantID == id {
			delete(m.bindings, bid)
		}
	}
	return nil
}

func (m *MockAssistantStore) CreateBinding(_ context.Context, in coreassistant.NewBinding) (*coreassistant.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	a := m.assistants[in.AssistantID]
	if a == nil || m.deleted[in.AssistantID] {
		return nil, coreassistant.ErrNotFound
	}
	for _, b := range m.bindings {
		if b.AssistantID == in.AssistantID {
			return nil, coreassistant.ErrAlreadyBound
		}
		if b.Platform == in.Platform && b.BotID == in.BotID {
			return nil, corechannel.ErrBotInUse
		}
	}
	m.next++
	b := &coreassistant.Binding{
		ID: fmt.Sprintf("bind%d", m.next), AssistantID: in.AssistantID, SpaceID: a.SpaceID,
		Platform: in.Platform, BotID: in.BotID, BotHandle: in.BotHandle, CreatedBy: in.CreatedBy,
		CreatedAt: time.Now().UTC(),
	}
	m.bindings[b.ID] = b
	m.tokens[b.ID] = in.Token
	out := *b
	return &out, nil
}

func (m *MockAssistantStore) GetBinding(_ context.Context, id string) (*coreassistant.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	if b := m.bindings[id]; b != nil {
		out := *b
		return &out, nil
	}
	return nil, nil
}

func (m *MockAssistantStore) GetBindingByAssistant(_ context.Context, assistantID string) (*coreassistant.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	for _, b := range m.bindings {
		if b.AssistantID == assistantID {
			out := *b
			return &out, nil
		}
	}
	return nil, nil
}

func (m *MockAssistantStore) DeleteBindingByAssistant(_ context.Context, assistantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	for id, b := range m.bindings {
		if b.AssistantID == assistantID {
			delete(m.bindings, id)
			return nil
		}
	}
	return coreassistant.ErrNotBound
}

func (m *MockAssistantStore) ListBindings(_ context.Context) ([]coreassistant.Binding, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	var out []coreassistant.Binding
	for _, b := range m.bindings {
		out = append(out, *b)
	}
	return out, nil
}

func (m *MockAssistantStore) BindingToken(_ context.Context, id string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	t, ok := m.tokens[id]
	if !ok {
		return "", coreassistant.ErrNotBound
	}
	return t, nil
}

var _ coreassistant.Store = (*MockAssistantStore)(nil)
