package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTriggerAgentCreatesATask(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"tk_1","space_id":"tm_1","status":"PENDING","last_run_id":"tr_1"}`))
	}))
	defer srv.Close()

	task, err := NewClient(srv.URL).TriggerAgent(t.Context(), "tok", "tm_1", "ag_1", "do the thing")
	if err != nil {
		t.Fatalf("TriggerAgent: %v", err)
	}
	if gotPath != "/api/spaces/tm_1/agents/ag_1/tasks" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["input"] != "do the thing" {
		t.Fatalf("input = %v", gotBody["input"])
	}
	if task.ID != "tk_1" || task.Status != "PENDING" || task.LastRunID == nil || *task.LastRunID != "tr_1" {
		t.Fatalf("task = %+v", task)
	}
}

// FindAgent fans out across the caller's spaces and resolves a name to the one
// space that holds it.
func TestFindAgentResolvesByNameAcrossSpaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/spaces":
			_, _ = w.Write([]byte(`[{"id":"tm_1","name":"One"},{"id":"tm_2","name":"Two"}]`))
		case strings.HasSuffix(r.URL.Path, "/agents") && strings.Contains(r.URL.Path, "tm_1"):
			_, _ = w.Write([]byte(`[{"id":"ag_1","name":"reviewer"}]`))
		case strings.HasSuffix(r.URL.Path, "/agents"):
			_, _ = w.Write([]byte(`[{"id":"ag_2","name":"builder"}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	agent, err := NewClient(srv.URL).FindAgent(t.Context(), "tok", "", "builder")
	if err != nil {
		t.Fatalf("FindAgent: %v", err)
	}
	if agent.ID != "ag_2" || agent.SpaceID != "tm_2" {
		t.Fatalf("agent = %+v", agent)
	}
}

// A name that matches in more than one space is refused rather than guessed, so
// a run never starts against the wrong agent.
func TestFindAgentAmbiguousNameIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/spaces":
			_, _ = w.Write([]byte(`[{"id":"tm_1","name":"One"},{"id":"tm_2","name":"Two"}]`))
		case strings.HasSuffix(r.URL.Path, "/agents"):
			_, _ = w.Write([]byte(`[{"id":"ag_x","name":"dup"}]`))
		}
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL).FindAgent(t.Context(), "tok", "", "dup"); err == nil {
		t.Fatal("an ambiguous name should be refused")
	}
}

func TestGetTaskReadsStatusAndOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/spaces/tm_1/tasks/tk_1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"tk_1","status":"SUCCEEDED","output":"done"}`))
	}))
	defer srv.Close()

	task, err := NewClient(srv.URL).GetTask(t.Context(), "tok", "tm_1", "tk_1")
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Status != "SUCCEEDED" || task.Output == nil || *task.Output != "done" {
		t.Fatalf("task = %+v", task)
	}
}
