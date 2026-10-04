package worker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/mock"
)

// First fetch: a run created by an active service account starts like any
// other; one whose service account was disabled is told to stop.
func TestGetWorkerTaskRunHandler_ServiceAccountInitiator(t *testing.T) {
	const taskRunID, space = "run-svc", "tm_1"
	disabledAt := time.Unix(1, 0).UTC()
	for _, tc := range []struct {
		name       string
		disabled   *time.Time
		wantCancel bool
	}{
		{"active", nil, false},
		{"disabled", &disabledAt, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runs := &mock.MockTaskRunStore{
				Runs: []coretask.Run{{
					ID: taskRunID, TaskID: "task-1", Input: "input",
					Status: string(coretask.RunStatusScheduled), CreatedBy: "u_svc",
				}},
				TaskList: []coretask.Task{{ID: "task-1", SpaceID: space, CreatedBy: "u_svc"}},
			}
			users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{
				"u_svc": {ID: "u_svc", Kind: coreidentity.KindService, DisabledAt: tc.disabled},
			}}
			spaces := &mock.MockSpaceStore{Members: []corespace.Member{
				{SpaceID: space, UserID: "u_svc", Role: corespace.RoleMember},
			}}
			h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs, Eligible: eligibility.New(users, spaces)})
			mux := http.NewServeMux()
			h.Register(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+taskRunID, nil)
			req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
			}
			var got workerclient.GetTaskRunResponse
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got.Run.CancelRequested != tc.wantCancel {
				t.Fatalf("cancel_requested = %v, want %v", got.Run.CancelRequested, tc.wantCancel)
			}
			if tc.wantCancel && runs.Runs[0].CancelReason != coretask.CancelReasonCreatorDisabled {
				t.Errorf("cancel_reason = %q, want creator_disabled", runs.Runs[0].CancelReason)
			}
		})
	}
}
