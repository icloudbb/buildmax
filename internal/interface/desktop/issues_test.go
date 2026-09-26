package desktop

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
)

// fakeIssueServer serves two spaces' issue routes the way the Space API does,
// enough for the Desktop Issues bindings. brokenSpace, when set, fails its
// listings so the inbox's partial-failure reporting can be seen.
type fakeIssueServer struct {
	mu          sync.Mutex
	brokenSpace string
	patches     []map[string]any
	comments    []map[string]any
}

func (f *fakeIssueServer) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == "/api/spaces":
			_, _ = w.Write([]byte(`[{"id":"s_a","name":"Alpha"},{"id":"s_b","name":"Beta"}]`))
		case strings.HasSuffix(path, "/issues") && r.Method == http.MethodGet:
			space := strings.Split(path, "/")[3]
			if space == f.brokenSpace {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":"boom"}`))
				return
			}
			if r.URL.Query().Get("owner") != "me" && r.URL.Query().Get("parent_id") == "" {
				t.Errorf("inbox listing without owner=me: %s", r.URL.RawQuery)
			}
			if parent := r.URL.Query().Get("parent_id"); parent != "" {
				_, _ = w.Write([]byte(`{"issues":[{"id":"i_child","title":"Child","status":"done","version":1,"updated_at":"2026-09-01T00:00:00Z"}],"total":1}`))
				return
			}
			status := r.URL.Query().Get("status")
			switch {
			case space == "s_a" && status == "todo":
				_, _ = w.Write([]byte(`{"issues":[{"id":"i_old","title":"Older","status":"todo","version":2,"updated_at":"2026-09-01T00:00:00Z"}],"total":1}`))
			case space == "s_b" && status == "in_progress":
				_, _ = w.Write([]byte(`{"issues":[{"id":"i_new","title":"Newer","status":"in_progress","version":5,"updated_at":"2026-09-20T00:00:00Z"}],"total":1}`))
			default:
				_, _ = w.Write([]byte(`{"issues":[],"total":0}`))
			}
		case strings.HasSuffix(path, "/comments") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"comments":[
				{"author_kind":"user","author_id":"u_1","body":"mine","created_at":"2026-09-02T00:00:00Z"},
				{"author_kind":"user","author_id":"u_2","body":"theirs","created_at":"2026-09-03T00:00:00Z"},
				{"author_kind":"local_agent","author_id":"u_1","body":"agent report","created_at":"2026-09-04T00:00:00Z"}],"total":3}`))
		case strings.HasSuffix(path, "/comments") && r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.comments = append(f.comments, body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"ic_1"}`))
		case r.Method == http.MethodPatch:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.patches = append(f.patches, body)
			if body["version"] != float64(5) {
				w.WriteHeader(http.StatusConflict)
				_, _ = w.Write([]byte(`{"error":"issue changed"}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"i_new","title":"Newer","status":"done","version":6,"updated_at":"2026-09-21T00:00:00Z"}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"id":"i_new","title":"Newer","description":"Do the thing.","status":"in_progress","version":5,"updated_at":"2026-09-20T00:00:00Z"}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func startIssueServer(t *testing.T) *fakeIssueServer {
	t.Helper()
	fake := &fakeIssueServer{}
	srv := httptest.NewServer(fake.handler(t))
	t.Cleanup(srv.Close)
	signInTo(t, srv.URL)
	return fake
}

// The inbox gathers open work across spaces, newest first, and carries the
// space each Issue belongs to, since every later call needs it.
func TestListMyIssuesGathersOpenWorkAcrossSpaces(t *testing.T) {
	startIssueServer(t)

	inbox, err := NewApp().ListMyIssues()
	if err != nil {
		t.Fatalf("ListMyIssues: %v", err)
	}
	if len(inbox.Issues) != 2 || len(inbox.Warnings) != 0 {
		t.Fatalf("inbox = %+v", inbox)
	}
	if inbox.Issues[0].ID != "i_new" || inbox.Issues[0].SpaceName != "Beta" || inbox.Issues[0].Version != 5 {
		t.Errorf("first = %+v, want the newest Issue with its space and version", inbox.Issues[0])
	}
	if inbox.Issues[1].ID != "i_old" || inbox.Issues[1].SpaceID != "s_a" {
		t.Errorf("second = %+v", inbox.Issues[1])
	}
}

// A space that cannot be read is named, not silently dropped: a partial inbox
// must not pass for a whole one.
func TestListMyIssuesNamesASpaceItCouldNotRead(t *testing.T) {
	fake := startIssueServer(t)
	fake.brokenSpace = "s_a"

	inbox, err := NewApp().ListMyIssues()
	if err != nil {
		t.Fatalf("ListMyIssues: %v", err)
	}
	if len(inbox.Issues) != 1 || len(inbox.Warnings) != 1 || !strings.Contains(inbox.Warnings[0], "Alpha") {
		t.Fatalf("inbox = %+v, want Beta's Issue and one warning naming Alpha", inbox)
	}
}

func TestListMyIssuesRequiresALogin(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	if _, err := NewApp().ListMyIssues(); err == nil || !strings.Contains(err.Error(), "sign in") {
		t.Fatalf("err = %v, want a sign-in message", err)
	}
}

// Detail carries the bounded thread, and marks the signed-in person's own
// comments — but not a local agent's report posted under the same login.
func TestGetIssueDetailMarksThePersonsOwnComments(t *testing.T) {
	startIssueServer(t)

	detail, err := NewApp().GetIssueDetail("s_b", "Beta", "i_new")
	if err != nil {
		t.Fatalf("GetIssueDetail: %v", err)
	}
	if detail.Description != "Do the thing." || len(detail.Children) != 1 || detail.Issue.SpaceName != "Beta" {
		t.Fatalf("detail = %+v", detail)
	}
	mine := []bool{}
	for _, c := range detail.Comments {
		mine = append(mine, c.Mine)
	}
	if len(mine) != 3 || !mine[0] || mine[1] || mine[2] {
		t.Fatalf("mine = %v, want only the person's own user comment", mine)
	}
}

// A stale version is an expected conflict, reported without an error so the
// view reloads instead of showing a failure.
func TestSetIssueStatusReportsAStaleVersionAsAConflict(t *testing.T) {
	fake := startIssueServer(t)
	app := NewApp()

	res, err := app.SetIssueStatus("s_b", "Beta", "i_new", "done", 4)
	if err != nil || !res.Conflict || res.Issue != nil {
		t.Fatalf("stale: res = %+v, err = %v, want a conflict", res, err)
	}
	res, err = app.SetIssueStatus("s_b", "Beta", "i_new", "done", 5)
	if err != nil || res.Conflict || res.Issue == nil || res.Issue.Status != "done" || res.Issue.Version != 6 {
		t.Fatalf("current: res = %+v, err = %v", res, err)
	}
	if len(fake.patches) != 2 || fake.patches[1]["status"] != "done" || len(fake.patches[1]) != 2 {
		t.Fatalf("patches = %v, want only version and status sent", fake.patches)
	}
}

// A comment typed in Desktop is the person's, so it carries no author_kind.
func TestCommentOnIssuePostsAsThePerson(t *testing.T) {
	fake := startIssueServer(t)
	app := NewApp()

	if err := app.CommentOnIssue("s_b", "i_new", "   "); err == nil {
		t.Fatal("an empty comment was accepted")
	}
	if err := app.CommentOnIssue("s_b", "i_new", "Fixed locally; see the PR."); err != nil {
		t.Fatalf("CommentOnIssue: %v", err)
	}
	if len(fake.comments) != 1 || fake.comments[0]["body"] != "Fixed locally; see the PR." {
		t.Fatalf("comments = %v", fake.comments)
	}
	if _, present := fake.comments[0]["author_kind"]; present {
		t.Fatalf("author_kind sent: %v", fake.comments[0])
	}
}
