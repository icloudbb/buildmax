package mock

import (
	"context"
	"errors"
	"fmt"
	"time"

	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// MockLLMModelStore is an in-memory LLMModelStore for tests.
//
// Credentials are kept in a separate map, mirroring the real store: reading a
// model and reading its key are different operations, and a mock that stapled
// the key onto the record would let a handler test pass while the endpoint
// serialized it.
type MockLLMModelStore struct {
	Models      []coregw.Model
	Credentials map[string]string
	Err         error
	next        int
}

func (m *MockLLMModelStore) CreateLLMModel(_ context.Context, in coregw.CreateModelInput) (*coregw.Model, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for _, existing := range m.Models {
		if existing.Name == in.Name {
			return nil, coregw.ErrModelNameTaken
		}
	}
	m.next++
	created := coregw.Model{
		ID:            fmt.Sprintf("lm_mock_%d", m.next),
		Name:          in.Name,
		ProviderType:  in.ProviderType,
		APIURL:        in.APIURL,
		Model:         in.Model,
		ContextWindow: in.ContextWindow,
		CallTimeout:   in.CallTimeout,
		MaxTokens:     in.MaxTokens,
		Reasoning:     in.Reasoning,
		CacheMode:     in.CacheMode,
		CacheTTL:      in.CacheTTL,
		Vision:        in.Vision,
		Capabilities:  in.Capabilities,
		Enabled:       true,
	}
	m.Models = append(m.Models, created)
	if m.Credentials == nil {
		m.Credentials = make(map[string]string)
	}
	m.Credentials[created.ID] = in.APIKey
	return &created, nil
}

func (m *MockLLMModelStore) GetLLMModel(_ context.Context, llmModelID string) (*coregw.Model, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	for i := range m.Models {
		if m.Models[i].ID == llmModelID {
			found := m.Models[i]
			return &found, nil
		}
	}
	return nil, nil
}

func (m *MockLLMModelStore) GetLLMModelByName(_ context.Context, name string) (*coregw.Model, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	if name == "" {
		return nil, nil
	}
	for i := range m.Models {
		if m.Models[i].Name == name {
			found := m.Models[i]
			return &found, nil
		}
	}
	return nil, nil
}

func (m *MockLLMModelStore) ListLLMModels(_ context.Context) ([]coregw.Model, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return append([]coregw.Model(nil), m.Models...), nil
}

func (m *MockLLMModelStore) SetLLMModelEnabled(_ context.Context, llmModelID string, enabled bool) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Models {
		if m.Models[i].ID == llmModelID {
			m.Models[i].Enabled = enabled
			return nil
		}
	}
	return nil
}

func (m *MockLLMModelStore) SetLLMModelCredential(_ context.Context, llmModelID, apiKey string) error {
	if m.Err != nil {
		return m.Err
	}
	for i := range m.Models {
		if m.Models[i].ID == llmModelID {
			if m.Credentials == nil {
				m.Credentials = map[string]string{}
			}
			m.Credentials[llmModelID] = apiKey
			m.Models[i].UpdatedAt = m.Models[i].UpdatedAt.Add(time.Second)
			return nil
		}
	}
	return errors.New("model not found")
}

func (m *MockLLMModelStore) LLMModelCredential(_ context.Context, llmModelID string) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	return m.Credentials[llmModelID], nil
}
