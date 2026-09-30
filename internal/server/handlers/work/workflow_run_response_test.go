package work

import (
	"encoding/json"
	"strings"
	"testing"

	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// The run's failure class is what a member reads to learn why a Workflow run
// failed without opening its nodes, so the response must carry it.
func TestWorkflowRunResponseCarriesFailureClass(t *testing.T) {
	failed := workflowRunToResponse(coreworkflow.Run{ID: "run", Status: "failed", FailureClass: string(coreworkflow.FailureRunDeadline)})
	body, err := json.Marshal(failed)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"failure_class":"run_deadline"`) {
		t.Fatalf("failed run response = %s, want failure_class run_deadline", body)
	}

	running, err := json.Marshal(workflowRunToResponse(coreworkflow.Run{ID: "run", Status: "running"}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(running), "failure_class") {
		t.Fatalf("running run response = %s, want no failure_class", running)
	}
}
