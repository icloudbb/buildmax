package httputil

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	"github.com/icloudbb/buildmax/internal/service/issue"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

func write(t *testing.T, err error) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	w := httptest.NewRecorder()
	return w, WriteServiceError(w, err)
}

func body(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q: %v", w.Body.String(), err)
	}
	return out.Error
}

func TestKindDecidesStatus(t *testing.T) {
	for _, tc := range []struct {
		kind apierr.Kind
		want int
	}{
		{apierr.KindNotConfigured, http.StatusServiceUnavailable},
		{apierr.KindInvalid, http.StatusBadRequest},
		{apierr.KindNotFound, http.StatusNotFound},
		{apierr.KindForbidden, http.StatusForbidden},
		{apierr.KindConflict, http.StatusConflict},
		{apierr.KindQuotaExceeded, http.StatusTooManyRequests},
		{apierr.KindUnavailable, http.StatusServiceUnavailable},
	} {
		w, handled := write(t, apierr.New(tc.kind, "refused"))
		if !handled {
			t.Fatalf("%s was not answered", tc.kind)
		}
		if w.Code != tc.want {
			t.Errorf("%s = %d, want %d", tc.kind, w.Code, tc.want)
		}
	}
}

// The report that prompted this could not tell whether these two differed on
// purpose or had drifted apart, because they lived in separate hand-written
// tables. They differ on purpose: in issues the workflow is a field of the
// request body, in workflows it is the resource the path addresses.
func TestSameNameDifferentKindAnswersDifferently(t *testing.T) {
	w, _ := write(t, issue.ErrWorkflowNotFound)
	if w.Code != http.StatusBadRequest {
		t.Errorf("issue.ErrWorkflowNotFound = %d, want 400: the request body named it", w.Code)
	}

	w, _ = write(t, workflow.ErrWorkflowNotFound)
	if w.Code != http.StatusNotFound {
		t.Errorf("workflow.ErrWorkflowNotFound = %d, want 404: the path addressed it", w.Code)
	}
}

// A service wrapping for its own context must not change the answer, and must
// not put its wrapper text in front of the caller.
func TestWrappedErrorKeepsStatusAndMessage(t *testing.T) {
	err := fmt.Errorf("load the workflow behind this run: %w", workflow.ErrWorkflowNotFound)

	w, handled := write(t, err)

	if !handled || w.Code != http.StatusNotFound {
		t.Fatalf("handled=%v status=%d", handled, w.Code)
	}
	if got := body(t, w); got != "workflow not found" {
		t.Errorf("body = %q, want the sentinel's own message", got)
	}
}

// The one sentinel that deliberately tells the caller more than its own text.
func TestDetailReachesTheCaller(t *testing.T) {
	err := apierr.Detail(workflow.ErrInvalidDefinition, "%v", errors.New("unexpected end of JSON input"))

	w, _ := write(t, err)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if got := body(t, w); got != "invalid workflow definition: unexpected end of JSON input" {
		t.Errorf("body = %q, want the parse error appended", got)
	}
}

// An unclassified error is a bug, not a refusal. Answering it here would turn a
// server fault into a 400 and hide it.
func TestUnclassifiedErrorIsNotAnswered(t *testing.T) {
	w, handled := write(t, errors.New("connection reset"))

	if handled {
		t.Errorf("a plain error must be left to the caller, got status %d", w.Code)
	}
}

// Retrying a task with nothing to retry is a state conflict, not a bad request.
func TestRetryRefusalsAreConflicts(t *testing.T) {
	for _, err := range []error{task.ErrNoRunToRetry, task.ErrRetryOfWorkflowStep} {
		w, _ := write(t, err)
		if w.Code != http.StatusConflict {
			t.Errorf("%v = %d, want 409", err, w.Code)
		}
	}
}
