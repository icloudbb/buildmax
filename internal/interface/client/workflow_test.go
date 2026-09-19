package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunWorkflowStartsARun(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.ContentLength > 0 {
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"run":{"id":"wr_1","workflow_id":"wf_1","status":"running"},"steps":[]}`))
	}))
	defer srv.Close()

	run, err := NewClient(srv.URL).RunWorkflow(t.Context(), "tok", "tm_1", "wf_1", `{"topic":"x"}`, "")
	if err != nil {
		t.Fatalf("RunWorkflow: %v", err)
	}
	if gotPath != "/api/spaces/tm_1/workflows/wf_1/runs" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotBody["input"] == nil {
		t.Fatalf("input not sent: %v", gotBody)
	}
	if run.ID != "wr_1" || run.Status != "running" {
		t.Fatalf("run = %+v", run)
	}
}

// A malformed --input is refused before the request, so the caller gets a clear
// message rather than an opaque server 400.
func TestRunWorkflowRejectsInvalidInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("request should not be sent for invalid input")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL).RunWorkflow(t.Context(), "tok", "tm_1", "wf_1", "not json", ""); err == nil {
		t.Fatal("invalid JSON input should be refused")
	}
}

func TestFindWorkflowResolvesByNameAndRefusesAmbiguity(t *testing.T) {
	newSrv := func(secondName string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/spaces":
				_, _ = w.Write([]byte(`[{"id":"tm_1","name":"One"},{"id":"tm_2","name":"Two"}]`))
			case strings.Contains(r.URL.Path, "tm_1"):
				_, _ = w.Write([]byte(`{"workflows":[{"id":"wf_1","name":"nightly","status":"published"}]}`))
			default:
				_, _ = w.Write([]byte(`{"workflows":[{"id":"wf_2","name":"` + secondName + `","status":"draft"}]}`))
			}
		}))
	}

	srv := newSrv("weekly")
	defer srv.Close()
	wf, err := NewClient(srv.URL).FindWorkflow(t.Context(), "tok", "", "nightly")
	if err != nil {
		t.Fatalf("FindWorkflow: %v", err)
	}
	if wf.ID != "wf_1" || wf.SpaceID != "tm_1" || wf.Status != "published" {
		t.Fatalf("workflow = %+v", wf)
	}

	dup := newSrv("nightly")
	defer dup.Close()
	if _, err := NewClient(dup.URL).FindWorkflow(t.Context(), "tok", "", "nightly"); err == nil {
		t.Fatal("an ambiguous name should be refused")
	}
}

func TestGetWorkflowRunReadsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/spaces/tm_1/workflow-runs/wr_1" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"run":{"id":"wr_1","status":"succeeded"},"steps":[]}`))
	}))
	defer srv.Close()
	run, err := NewClient(srv.URL).GetWorkflowRun(t.Context(), "tok", "tm_1", "wr_1")
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if run.Status != "succeeded" {
		t.Fatalf("run = %+v", run)
	}
}
