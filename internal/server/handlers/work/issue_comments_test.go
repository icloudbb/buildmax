package work

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

const commentTestSecret = "comment-test-secret"

const (
	commentSpace      = "tm_comments"
	commentOtherSpace = "tm_comments_other"
)

// commentMux wires an issue in commentSpace and one in commentOtherSpace, so
// every test can ask both "does this work" and "does it leak".
func commentMux(t *testing.T) (*http.ServeMux, *mock.MockIssueStore, *mock.MockIssueCommentStore) {
	t.Helper()
	issues := commentIssues()
	comments := &mock.MockIssueCommentStore{}
	spaces := commentSpaces()
	h := New(Config{
		JWTSecret:     commentTestSecret,
		Spaces:        spaces,
		Issues:        issues,
		IssueComments: comments,
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, issues, comments
}

func commentIssues() *mock.MockIssueStore {
	return &mock.MockIssueStore{
		Issues: []coreissue.Issue{
			{ID: "i_1", UserID: "u_owner", SpaceID: commentSpace, Title: "Parent", Status: coreissue.StatusTodo},
			{ID: "i_2", UserID: "u_owner", SpaceID: commentSpace, Title: "Other issue", Status: coreissue.StatusTodo},
			{ID: "i_far", UserID: "u_stranger", SpaceID: commentOtherSpace, Title: "Theirs", Status: coreissue.StatusTodo},
		},
	}
}

func commentSpaces() *mock.MockSpaceStore {
	return &mock.MockSpaceStore{
		Spaces: []corespace.Space{
			{ID: commentSpace, Name: "Comments", CreatedBy: "u_owner"},
			{ID: commentOtherSpace, Name: "Other", CreatedBy: "u_stranger"},
		},
		Members: []corespace.Member{
			{SpaceID: commentSpace, UserID: "u_owner", Role: corespace.RoleOwner},
			{SpaceID: commentSpace, UserID: "u_member", Role: corespace.RoleMember},
			{SpaceID: commentOtherSpace, UserID: "u_stranger", Role: corespace.RoleOwner},
		},
	}
}

func commentRequest(t *testing.T, mux *http.ServeMux, method, path, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(userID, commentTestSecret))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestIssueComments_CreateListEditDelete(t *testing.T) {
	mux, _, store := commentMux(t)
	base := "/api/spaces/" + commentSpace + "/issues/i_1/comments"

	rec := commentRequest(t, mux, http.MethodPost, base, "u_member", `{"body":"blocked on the vendor"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body=%s", rec.Code, rec.Body.String())
	}
	var created issueCommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.AuthorKind != coreissue.CommentAuthorUser || created.AuthorID != "u_member" {
		t.Fatalf("author = %s/%s, want user/u_member", created.AuthorKind, created.AuthorID)
	}

	rec = commentRequest(t, mux, http.MethodGet, base, "u_owner", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	var list issueCommentListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 || len(list.Comments) != 1 {
		t.Fatalf("list = %+v", list)
	}

	rec = commentRequest(t, mux, http.MethodPatch, base+"/"+created.ID, "u_member", `{"body":"unblocked"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	var edited issueCommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &edited); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if edited.Body != "unblocked" || edited.EditedAt == nil {
		t.Fatalf("edited = %+v, want the new body and an edited_at", edited)
	}

	rec = commentRequest(t, mux, http.MethodDelete, base+"/"+created.ID, "u_member", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204, body=%s", rec.Code, rec.Body.String())
	}
	if len(store.Comments) != 0 {
		t.Fatalf("delete left %d comments", len(store.Comments))
	}
}

func TestIssueComments_BodyRequired(t *testing.T) {
	mux, _, _ := commentMux(t)
	rec := commentRequest(t, mux, http.MethodPost, "/api/spaces/"+commentSpace+"/issues/i_1/comments", "u_member", `{"body":"  "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestIssueComments_TooLong(t *testing.T) {
	mux, _, _ := commentMux(t)
	body, err := json.Marshal(map[string]string{"body": strings.Repeat("x", 17*1024)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	rec := commentRequest(t, mux, http.MethodPost, "/api/spaces/"+commentSpace+"/issues/i_1/comments", "u_member", string(body))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

// The issue is what authorizes a comment request, so an issue in another space
// is not found even though the comment routes never name a space resource of
// their own.
func TestIssueComments_IssueInAnotherSpace(t *testing.T) {
	mux, _, _ := commentMux(t)
	rec := commentRequest(t, mux, http.MethodGet, "/api/spaces/"+commentSpace+"/issues/i_far/comments", "u_owner", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// A comment ID from a different issue must not be reachable through this
// issue's path, or the issue check would authorize a write somewhere else.
func TestIssueComments_CommentFromAnotherIssue(t *testing.T) {
	mux, _, _ := commentMux(t)
	rec := commentRequest(t, mux, http.MethodPost, "/api/spaces/"+commentSpace+"/issues/i_2/comments", "u_member", `{"body":"on issue two"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup create status = %d", rec.Code)
	}
	var created issueCommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec = commentRequest(t, mux, http.MethodPatch, "/api/spaces/"+commentSpace+"/issues/i_1/comments/"+created.ID, "u_member", `{"body":"moved"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-issue patch status = %d, want 404", rec.Code)
	}
	rec = commentRequest(t, mux, http.MethodDelete, "/api/spaces/"+commentSpace+"/issues/i_1/comments/"+created.ID, "u_member", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-issue delete status = %d, want 404", rec.Code)
	}
}

func TestIssueComments_EditingSomeoneElsesIsForbidden(t *testing.T) {
	mux, _, _ := commentMux(t)
	base := "/api/spaces/" + commentSpace + "/issues/i_1/comments"
	rec := commentRequest(t, mux, http.MethodPost, base, "u_member", `{"body":"mine"}`)
	var created issueCommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Even the space owner, who may delete this comment, may not rewrite it.
	rec = commentRequest(t, mux, http.MethodPatch, base+"/"+created.ID, "u_owner", `{"body":"not mine"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("owner patch status = %d, want 403", rec.Code)
	}
}

func TestIssueComments_DeleteAuthorization(t *testing.T) {
	mux, _, store := commentMux(t)
	base := "/api/spaces/" + commentSpace + "/issues/i_1/comments"
	rec := commentRequest(t, mux, http.MethodPost, base, "u_member", `{"body":"mine"}`)
	var created issueCommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The owner holds moderate_issue_comments, so they may remove it.
	if rec := commentRequest(t, mux, http.MethodDelete, base+"/"+created.ID, "u_owner", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("owner delete status = %d, want 204", rec.Code)
	}
	if len(store.Comments) != 0 {
		t.Fatalf("owner delete left %d comments", len(store.Comments))
	}

	// A plain member does not, so someone else's comment stays put.
	rec = commentRequest(t, mux, http.MethodPost, base, "u_owner", `{"body":"the owner's"}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec := commentRequest(t, mux, http.MethodDelete, base+"/"+created.ID, "u_member", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("member delete status = %d, want 403", rec.Code)
	}
	if len(store.Comments) != 1 {
		t.Fatalf("refused delete removed the comment anyway")
	}
}

// Without a comment store the issue routes must keep working; comments answer
// 503 rather than the deployment losing its issues.
func TestIssueComments_NotConfigured(t *testing.T) {
	spaces := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: commentSpace, Name: "Comments", CreatedBy: "u_owner"}},
		Members: []corespace.Member{{SpaceID: commentSpace, UserID: "u_owner", Role: corespace.RoleOwner}},
	}
	h := New(Config{
		JWTSecret: commentTestSecret,
		Spaces:    spaces,
		Issues:    &mock.MockIssueStore{Issues: []coreissue.Issue{{ID: "i_1", SpaceID: commentSpace}}},
	})
	mux := http.NewServeMux()
	h.Register(mux)

	rec := commentRequest(t, mux, http.MethodGet, "/api/spaces/"+commentSpace+"/issues/i_1/comments", "u_owner", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("comments status = %d, want 503", rec.Code)
	}
	rec = commentRequest(t, mux, http.MethodGet, "/api/spaces/"+commentSpace+"/issues/i_1", "u_owner", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("issue status = %d, want 200 — issues must survive without comments", rec.Code)
	}
}

// A person may relay what an agent on their machine said, and the thread
// records it as that: a claim, with the person who made it as the author. It is
// not stored as `agent`, which is what a run this deployment scheduled and
// recorded writes through a run token.
func TestIssueComments_LocalAgentReport(t *testing.T) {
	mux, _, comments := commentMux(t)
	base := "/api/spaces/" + commentSpace + "/issues/i_1/comments"
	rec := commentRequest(t, mux, http.MethodPost, base, "u_member",
		`{"body":"Adapter written and tested.","author_kind":"local_agent"}`)
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
	if got.AuthorKind != coreissue.CommentAuthorLocalAgent {
		t.Fatalf("author_kind = %q, want %q", got.AuthorKind, coreissue.CommentAuthorLocalAgent)
	}
	if got.AuthorID != "u_member" {
		t.Fatalf("author_id = %q, want the person who reported it", got.AuthorID)
	}
	// No run produced this, so nothing may claim one did.
	if got.SourceTaskID != nil || got.SourceTaskRunID != nil {
		t.Fatalf("a local report names a run: %v/%v", got.SourceTaskID, got.SourceTaskRunID)
	}
}

// A session may not borrow the deployment's own voices. `agent` is written by a
// run token and `system` by the server, and either one arriving from a person's
// session would make the thread claim provenance nobody has.
func TestIssueComments_RefusesBorrowedAuthorKinds(t *testing.T) {
	mux, _, comments := commentMux(t)
	base := "/api/spaces/" + commentSpace + "/issues/i_1/comments"
	for _, kind := range []string{"agent", "system", "robot"} {
		rec := commentRequest(t, mux, http.MethodPost, base, "u_member",
			`{"body":"not mine to claim","author_kind":"`+kind+`"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("author_kind %q = %d, want 400, body=%s", kind, rec.Code, rec.Body.String())
		}
	}
	list, _, err := comments.ListIssueComments(t.Context(), "i_1", 10, 0)
	if err != nil {
		t.Fatalf("ListIssueComments: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("a refused claim still wrote a comment: %+v", list)
	}
}

// A local agent's report is the agent's statement, not the person's prose. The
// person who relayed it cannot then edit it into something else.
func TestIssueComments_LocalAgentReportIsNotEditable(t *testing.T) {
	mux, _, _ := commentMux(t)
	base := "/api/spaces/" + commentSpace + "/issues/i_1/comments"
	rec := commentRequest(t, mux, http.MethodPost, base, "u_member",
		`{"body":"Adapter written.","author_kind":"local_agent"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed: status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var created issueCommentResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	rec = commentRequest(t, mux, http.MethodPatch, base+"/"+created.ID, "u_member", `{"body":"rewritten"}`)
	if rec.Code == http.StatusOK {
		t.Fatal("a local agent report was edited by the person who relayed it")
	}
}
