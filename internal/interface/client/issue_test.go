package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
)

// The inbox asks each space the same question and keeps the space alongside each
// issue, because a local surface has no current space to put back later.
func TestListOwnedIssuesCarriesTheSpace(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/spaces":
			_, _ = w.Write([]byte(`[{"id":"tm_1","name":"Platform"},{"id":"tm_2","name":"Data"}]`))
		case strings.HasSuffix(r.URL.Path, "/issues"):
			asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
			if strings.Contains(r.URL.Path, "tm_1") {
				_, _ = w.Write([]byte(`{"issues":[{"id":"i_1","title":"Ship it","status":"todo"}],"total":1}`))
				return
			}
			_, _ = w.Write([]byte(`{"issues":[],"total":0}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	issues, problems := NewClient(srv.URL).ListOwnedIssues(t.Context(), "tok", "todo", 25)
	if len(problems) != 0 {
		t.Fatalf("problems = %v", problems)
	}
	if len(issues) != 1 || issues[0].Issue.ID != "i_1" {
		t.Fatalf("issues = %+v", issues)
	}
	if issues[0].SpaceID != "tm_1" || issues[0].SpaceName != "Platform" {
		t.Fatalf("space lost: %+v", issues[0])
	}
	if len(asked) != 2 {
		t.Fatalf("asked %d spaces, want 2: %v", len(asked), asked)
	}
	for _, url := range asked {
		if !strings.Contains(url, "owner=me") || !strings.Contains(url, "status=todo") {
			t.Fatalf("query lost a filter: %s", url)
		}
	}
}

// One unreadable space must not empty the inbox. The caller is told which space
// failed and still sees the rest.
func TestListOwnedIssuesSkipsASpaceItCannotRead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/spaces":
			_, _ = w.Write([]byte(`[{"id":"tm_1","name":"Platform"},{"id":"tm_gone","name":"Archived"}]`))
		case strings.Contains(r.URL.Path, "tm_gone"):
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"not a member"}`))
		default:
			_, _ = w.Write([]byte(`{"issues":[{"id":"i_1","title":"Ship it","status":"todo"}],"total":1}`))
		}
	}))
	defer srv.Close()

	issues, problems := NewClient(srv.URL).ListOwnedIssues(t.Context(), "tok", "", 0)
	if len(issues) != 1 {
		t.Fatalf("one space failing emptied the inbox: %+v", issues)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Error(), "Archived") {
		t.Fatalf("problems = %v, want one naming the space", problems)
	}
}

// A server that cannot even list spaces has no inbox to show, and says so rather
// than reporting an empty one.
func TestListOwnedIssuesReportsASpaceListingFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"token expired"}`))
	}))
	defer srv.Close()

	issues, problems := NewClient(srv.URL).ListOwnedIssues(t.Context(), "tok", "", 0)
	if len(issues) != 0 || len(problems) != 1 {
		t.Fatalf("issues = %v, problems = %v", issues, problems)
	}
	if !strings.Contains(problems[0].Error(), "list spaces") {
		t.Fatalf("problem does not say what failed: %v", problems[0])
	}
}

// CommentOnIssue is the CLI's report path: it posts to the issue's comment
// route as local_agent.
func TestCommentOnIssuePostsLocalAgent(t *testing.T) {
	var got map[string]any
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		path = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"ic_1"}`))
	}))
	defer srv.Close()

	if err := NewClient(srv.URL).CommentOnIssue(t.Context(), "tok", "tm_1", "i_1", "adapter shipped"); err != nil {
		t.Fatalf("CommentOnIssue: %v", err)
	}
	if path != "/api/spaces/tm_1/issues/i_1/comments" {
		t.Fatalf("path = %q", path)
	}
	if got["author_kind"] != coreissue.CommentAuthorLocalAgent {
		t.Fatalf("author_kind = %v, want %q", got["author_kind"], coreissue.CommentAuthorLocalAgent)
	}
	if got["body"] != "adapter shipped" {
		t.Fatalf("body = %v", got["body"])
	}
}

// A non-2xx from the comment route is surfaced as an error, not swallowed.
func TestCommentOnIssueSurfacesAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"run comment budget exhausted"}`))
	}))
	defer srv.Close()
	if err := NewClient(srv.URL).CommentOnIssue(t.Context(), "tok", "tm_1", "i_1", "one too many"); err == nil {
		t.Fatal("a 429 was read as success")
	}
}

// The version travels from the read to the write. A client that re-read it
// would turn the refusal a stale change deserves into a silent overwrite.
func TestSetIssueStatusCarriesTheVersionItWasRead(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("unexpected method %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sent); err != nil {
			t.Errorf("decode: %v", err)
		}
		_, _ = w.Write([]byte(`{"id":"i_1","status":"done","version":8}`))
	}))
	defer srv.Close()

	updated, err := NewClient(srv.URL).SetIssueStatus(t.Context(), "tok", "tm_1", "i_1", "done", 7)
	if err != nil {
		t.Fatalf("SetIssueStatus: %v", err)
	}
	if sent["version"] != float64(7) || sent["status"] != "done" {
		t.Fatalf("sent = %v", sent)
	}
	if updated.Version != 8 {
		t.Fatalf("version = %d, want the one the server returned", updated.Version)
	}
}

// A conflict is surfaced, not swallowed: somebody else moved the issue.
func TestSetIssueStatusSurfacesAConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"issue changed since it was read"}`))
	}))
	defer srv.Close()
	if _, err := NewClient(srv.URL).SetIssueStatus(t.Context(), "tok", "tm_1", "i_1", "done", 1); err == nil {
		t.Fatal("a refused status change was reported as applied")
	}
}

// FindIssue returns the issue with the space, because every caller needs it
// next -- to print it, or to read the version an update has to carry.
func TestFindIssueReturnsTheIssueWithItsSpace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/spaces":
			_, _ = w.Write([]byte(`[{"id":"tm_1","name":"Platform"},{"id":"tm_2","name":"Data"}]`))
		case strings.Contains(r.URL.Path, "tm_2"):
			_, _ = w.Write([]byte(`{"id":"i_1","title":"Ship it","status":"todo","version":3}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"issue not found"}`))
		}
	}))
	defer srv.Close()

	space, issue, err := NewClient(srv.URL).FindIssue(t.Context(), "tok", "i_1")
	if err != nil {
		t.Fatalf("FindIssue: %v", err)
	}
	if space.ID != "tm_2" || space.Name != "Data" {
		t.Fatalf("space = %+v", space)
	}
	if issue.Version != 3 {
		t.Fatalf("version = %d, want 3", issue.Version)
	}
}

func TestFindIssueSaysWhenNoSpaceHasIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/spaces" {
			_, _ = w.Write([]byte(`[{"id":"tm_1","name":"Platform"}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"issue not found"}`))
	}))
	defer srv.Close()
	if _, _, err := NewClient(srv.URL).FindIssue(t.Context(), "tok", "i_nope"); err == nil {
		t.Fatal("a missing issue resolved to a space")
	}
}

// A person's comment omits author_kind, so the server attributes it to the
// signed-in person rather than to a local agent.
func TestCommentAsPersonOmitsAuthorKind(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"ic_1"}`))
	}))
	defer srv.Close()

	if err := NewClient(srv.URL).CommentAsPerson(t.Context(), "tok", "tm_1", "i_1", "reviewed locally"); err != nil {
		t.Fatalf("CommentAsPerson: %v", err)
	}
	if _, present := got["author_kind"]; present {
		t.Fatalf("author_kind = %v, want it omitted", got["author_kind"])
	}
	if got["body"] != "reviewed locally" {
		t.Fatalf("body = %v", got["body"])
	}
}
