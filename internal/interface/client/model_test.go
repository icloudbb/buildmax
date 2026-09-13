package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/llm/models" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"id":"lm_1","name":"Fast","provider_type":"openai_compatible","model":"v/fast","enabled":true}],"default_model":"Fast"}`))
	}))
	defer srv.Close()

	models, def, err := NewClient(srv.URL).ListModels(context.Background(), "tok")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].Name != "Fast" || def != "Fast" {
		t.Fatalf("unexpected result: %+v default=%q", models, def)
	}
}

func TestClientCreateModelSendsBodyAndReturnsNoCredential(t *testing.T) {
	const secret = "sk-CLIENT-SECRET"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/llm/models" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		// The key must be in the body, never the query string.
		if r.URL.RawQuery != "" {
			t.Errorf("query string is not empty: %q", r.URL.RawQuery)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["api_key"] != secret {
			t.Errorf("api_key not forwarded in the body: %v", body["api_key"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// The response never carries the credential.
		_, _ = w.Write([]byte(`{"id":"lm_2","name":"New","provider_type":"openai_compatible","model":"v/new","enabled":true}`))
	}))
	defer srv.Close()

	created, err := NewClient(srv.URL).CreateModel(context.Background(), "tok", CreateModelInput{
		Name: "New", APIURL: "https://api.example.test/v1", Model: "v/new", APIKey: secret,
	})
	if err != nil {
		t.Fatalf("CreateModel: %v", err)
	}
	if created.ID != "lm_2" || created.Name != "New" {
		t.Fatalf("unexpected model: %+v", created)
	}
}

func TestClientCreateModelSurfacesEncryptionRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"no deployment encryption key is configured, so a model credential cannot be stored"}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).CreateModel(context.Background(), "tok", CreateModelInput{
		Name: "New", APIURL: "https://api.example.test/v1", Model: "v/new", APIKey: "sk-x",
	})
	if err == nil || !strings.Contains(err.Error(), "encryption key") {
		t.Fatalf("expected the encryption refusal to surface, got %v", err)
	}
}

func TestClientSetModelEnabled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/llm/models/lm_1/state" || r.Method != http.MethodPut {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"lm_1","name":"Fast","provider_type":"openai_compatible","model":"v/fast","enabled":false}`))
	}))
	defer srv.Close()

	updated, err := NewClient(srv.URL).SetModelEnabled(context.Background(), "tok", "lm_1", false)
	if err != nil {
		t.Fatalf("SetModelEnabled: %v", err)
	}
	if updated.Enabled {
		t.Fatalf("model still enabled: %+v", updated)
	}
}
