package worker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	issuesvc "github.com/icloudbb/buildmax/internal/service/issue"
	"github.com/icloudbb/buildmax/internal/util"
)

const issueWorkerSpace = "tm_llm"

// issueWorkerMux wires a run whose task names an issue, plus a parent issue
// with one child and one comment already on the thread.
func issueWorkerMux(t *testing.T, comments *mock.MockIssueCommentStore) (*http.ServeMux, *mock.MockIssueStore) {
	t.Helper()
	agentID := "a_1"
	issueID := "i_1"
	run := coretask.Run{ID: "run-1", TaskID: "task-1", Status: "RUNNING", CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: "task-1", ConversationID: "conv-1", SpaceID: issueWorkerSpace, CreatedBy: "u1", IssueID: &issueID, AgentID: &agentID}
	issues := &mock.MockIssueStore{
		Issues: []coreissue.Issue{
			{ID: issueID, UserID: "u1", SpaceID: issueWorkerSpace, Title: "Ship the importer", Description: "Import the bundle", Status: coreissue.StatusInProgress, Version: 1},
			{ID: "i_child", UserID: "u1", SpaceID: issueWorkerSpace, Title: "Write the adapter", Status: coreissue.StatusTodo, ParentIssueID: util.Ptr(issueID), Version: 1},
		},
	}
	h := New(Config{
		JWTSecret: workerTestSecret,
		TaskRuns:  &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}},
		Issues:    &issuesvc.Service{Issues: issues, Comments: comments},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, issues
}

func issueWorkerRequest(t *testing.T, mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, "run-1", "task-1"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestGetRunIssue(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	mux, _ := issueWorkerMux(t, comments)
	if _, err := comments.CreateIssueComment(t.Context(), coreissue.CreateCommentInput{
		IssueID: "i_1", AuthorKind: coreissue.CommentAuthorUser, AuthorID: "u1", Body: "Start with the adapter",
	}); err != nil {
		t.Fatalf("seed comment: %v", err)
	}

	rec := issueWorkerRequest(t, mux, http.MethodGet, "/api/worker/task-runs/run-1/issue", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out runIssueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Title != "Ship the importer" || out.Status != coreissue.StatusInProgress {
		t.Fatalf("issue = %+v", out)
	}
	if len(out.Children) != 1 || out.Children[0].Title != "Write the adapter" {
		t.Fatalf("children = %+v", out.Children)
	}
	if len(out.Comments) != 1 || out.Comments[0].AuthorKind != coreissue.CommentAuthorUser {
		t.Fatalf("comments = %+v", out.Comments)
	}
	if out.OmittedComments != 0 {
		t.Fatalf("omitted = %d, want 0", out.OmittedComments)
	}
}

// A thread longer than the window returns its tail and says how much it left
// out, rather than paging the agent through a space's history.
func TestGetRunIssueBoundsTheThread(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	mux, _ := issueWorkerMux(t, comments)
	total := issueCommentWindow + 5
	for i := range total {
		if _, err := comments.CreateIssueComment(t.Context(), coreissue.CreateCommentInput{
			IssueID: "i_1", AuthorKind: coreissue.CommentAuthorUser, AuthorID: "u1", Body: string(rune('a' + i%26)),
		}); err != nil {
			t.Fatalf("seed comment %d: %v", i, err)
		}
	}
	rec := issueWorkerRequest(t, mux, http.MethodGet, "/api/worker/task-runs/run-1/issue", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var out runIssueResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out.Comments) != issueCommentWindow {
		t.Fatalf("comments = %d, want %d", len(out.Comments), issueCommentWindow)
	}
	if out.OmittedComments != total-issueCommentWindow {
		t.Fatalf("omitted = %d, want %d", out.OmittedComments, total-issueCommentWindow)
	}
}

// The comment is authored the way RunReporter authors a finished run's summary,
// so a reader of the thread sees one kind of agent statement, not two.
func TestPostRunIssueComment(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	mux, _ := issueWorkerMux(t, comments)
	rec := issueWorkerRequest(t, mux, http.MethodPost, "/api/worker/task-runs/run-1/issue/comments",
		`{"body":"Adapter written and tested.","artifact_ids":["gsyt7at6cjfr33d73mta"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	list, _, err := comments.ListIssueComments(t.Context(), "i_1", 10, 0)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("comments = %d, want 1", len(list))
	}
	got := list[0]
	if got.AuthorKind != coreissue.CommentAuthorAgent || got.AuthorID != "a_1" {
		t.Fatalf("author = %s/%s", got.AuthorKind, got.AuthorID)
	}
	if got.SourceTaskID == nil || *got.SourceTaskID != "task-1" || got.SourceTaskRunID == nil || *got.SourceTaskRunID != "run-1" {
		t.Fatalf("source = %v/%v", got.SourceTaskID, got.SourceTaskRunID)
	}
	if !strings.Contains(got.Body, "gsyt7at6cjfr33d73mta") {
		t.Fatalf("body does not name the artifact: %q", got.Body)
	}
}

// A run may add only RunCommentBudget comments; the next is refused with 429
// rather than filling the thread.
func TestPostRunIssueCommentBudget(t *testing.T) {
	comments := &mock.MockIssueCommentStore{}
	mux, _ := issueWorkerMux(t, comments)
	for i := range issuesvc.RunCommentBudget {
		rec := issueWorkerRequest(t, mux, http.MethodPost, "/api/worker/task-runs/run-1/issue/comments",
			`{"body":"progress update"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("comment %d: status = %d, want 201, body=%s", i, rec.Code, rec.Body.String())
		}
	}
	rec := issueWorkerRequest(t, mux, http.MethodPost, "/api/worker/task-runs/run-1/issue/comments",
		`{"body":"one too many"}`)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-budget status = %d, want 429, body=%s", rec.Code, rec.Body.String())
	}
}

// A run whose task names no issue is refused rather than served someone else's.
func TestRunIssueRoutesRefuseARunWithNoIssue(t *testing.T) {
	run := coretask.Run{ID: "run-1", TaskID: "task-1", Status: "RUNNING", CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: "task-1", ConversationID: "conv-1", SpaceID: issueWorkerSpace, CreatedBy: "u1"}
	h := New(Config{
		JWTSecret: workerTestSecret,
		TaskRuns:  &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}},
		Issues:    &issuesvc.Service{Issues: &mock.MockIssueStore{}, Comments: &mock.MockIssueCommentStore{}},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/api/worker/task-runs/run-1/issue", ""},
		{http.MethodPost, "/api/worker/task-runs/run-1/issue/comments", `{"body":"anything"}`},
	} {
		rec := issueWorkerRequest(t, mux, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404, body=%s", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// Another run's token opens nothing here: every worker route is scoped to the
// run named in its path.
func TestRunIssueRejectsAnotherRunsToken(t *testing.T) {
	mux, _ := issueWorkerMux(t, &mock.MockIssueCommentStore{})
	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/run-1/issue", nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, "run-2", "task-2"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("another run's token was accepted, body=%s", rec.Body.String())
	}
}
