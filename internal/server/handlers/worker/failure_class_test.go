package worker

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/util"
)

func patchTerminal(t *testing.T, runs *mock.MockTaskRunStore, runID string, body workerclient.PatchTaskRunRequest) int {
	t.Helper()
	mux := http.NewServeMux()
	New(Config{JWTSecret: workerTestSecret, TaskRuns: runs}).Register(mux)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/worker/task-runs/"+runID, bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, runID, "task-1"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w.Code
}

func runStoreWith(status coretask.RunStatus) *mock.MockTaskRunStore {
	return &mock.MockTaskRunStore{
		Runs:     []coretask.Run{{ID: "run-1", TaskID: "task-1", Status: string(status)}},
		TaskList: []coretask.Task{{ID: "task-1", SpaceID: "tm_1", CreatedBy: "u1", Status: string(status)}},
	}
}

func TestPatchWorkerTaskRun_StoresTheReportedFailureClass(t *testing.T) {
	cases := []struct {
		name     string
		reported *string
		want     coretask.FailureClass
	}{
		{"named class", util.Ptr(string(coretask.FailureModel)), coretask.FailureModel},
		{"no class", nil, coretask.FailureUnclassified},
		{"unknown class", util.Ptr("mystery"), coretask.FailureUnclassified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runs := runStoreWith(coretask.RunStatusRunning)
			code := patchTerminal(t, runs, "run-1", workerclient.PatchTaskRunRequest{
				Status:       string(coretask.RunStatusFailed),
				ErrorMessage: util.Ptr("boom"),
				FailureClass: tc.reported,
			})
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200", code)
			}
			if got := runs.Runs[0].FailureClass; got != string(tc.want) {
				t.Errorf("failure_class = %q, want %q", got, tc.want)
			}
		})
	}
}

// A worker ends a run it has not claimed yet when the server could not give it
// a plugin, or when a cancel landed between dispatch and start. That report
// must land: otherwise the run stays SCHEDULED until the run timeout.
func TestPatchWorkerTaskRun_EndsARunRefusedBeforeItsClaim(t *testing.T) {
	runs := runStoreWith(coretask.RunStatusScheduled)
	code := patchTerminal(t, runs, "run-1", workerclient.PatchTaskRunRequest{
		Status:       string(coretask.RunStatusFailed),
		ErrorMessage: util.Ptr("plugin not activated"),
		FailureClass: util.Ptr(string(coretask.FailureSpaceConfiguration)),
	})
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if runs.Runs[0].Status != string(coretask.RunStatusFailed) {
		t.Fatalf("run status = %q, want FAILED", runs.Runs[0].Status)
	}
	if runs.Runs[0].FailureClass != string(coretask.FailureSpaceConfiguration) {
		t.Errorf("failure_class = %q, want space_configuration", runs.Runs[0].FailureClass)
	}

	canceled := runStoreWith(coretask.RunStatusScheduled)
	if code := patchTerminal(t, canceled, "run-1", workerclient.PatchTaskRunRequest{
		Status: string(coretask.RunStatusCanceled),
	}); code != http.StatusOK {
		t.Fatalf("cancel status = %d, want 200", code)
	}
	if canceled.Runs[0].Status != string(coretask.RunStatusCanceled) {
		t.Errorf("run status = %q, want CANCELED", canceled.Runs[0].Status)
	}
}

// A run cannot succeed without having been claimed: only an unclaimed run's
// refusal or cancel may skip RUNNING.
func TestPatchWorkerTaskRun_DoesNotSucceedAnUnclaimedRun(t *testing.T) {
	runs := runStoreWith(coretask.RunStatusScheduled)
	patchTerminal(t, runs, "run-1", workerclient.PatchTaskRunRequest{
		Status: string(coretask.RunStatusSucceeded),
		Output: util.Ptr("done"),
	})
	if runs.Runs[0].Status != string(coretask.RunStatusScheduled) {
		t.Errorf("run status = %q, want it left SCHEDULED", runs.Runs[0].Status)
	}
}
