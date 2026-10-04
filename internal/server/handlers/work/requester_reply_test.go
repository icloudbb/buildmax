package work

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/mock"
	assistantsvc "github.com/icloudbb/buildmax/internal/service/assistant"
)

type fakeReplier struct {
	cmds []assistantsvc.ReplyCmd
	err  error
}

func (f *fakeReplier) ReplyToRequester(_ context.Context, cmd assistantsvc.ReplyCmd) (*coreissue.Comment, error) {
	f.cmds = append(f.cmds, cmd)
	if f.err != nil {
		return nil, f.err
	}
	return &coreissue.Comment{ID: "c_1", IssueID: cmd.IssueID, AuthorKind: coreissue.CommentAuthorUser, AuthorID: cmd.ActorID, Body: cmd.Text}, nil
}

// A member's reply reaches the front door as theirs, for the path's Issue in
// the path's Space; its refusals keep their status; another Space's Issue is
// not found.
func TestReplyToRequesterRoute(t *testing.T) {
	replier := &fakeReplier{}
	h := New(Config{JWTSecret: commentTestSecret, Spaces: commentSpaces(), Issues: commentIssues(), IssueComments: &mock.MockIssueCommentStore{}, RequesterReplies: replier})
	routed := http.NewServeMux()
	h.Register(routed)
	post := func(path, user string) *httptest.ResponseRecorder {
		return commentRequest(t, routed, http.MethodPost, path, user, `{"text":"Fixed it."}`)
	}

	rec := post("/api/spaces/"+commentSpace+"/issues/i_1/requester-replies", "u_member")
	if rec.Code != http.StatusCreated || len(replier.cmds) != 1 {
		t.Fatalf("reply = %d %s", rec.Code, rec.Body.String())
	}
	if c := replier.cmds[0]; c.SpaceID != commentSpace || c.ActorID != "u_member" || c.IssueID != "i_1" || c.Text != "Fixed it." {
		t.Errorf("cmd = %+v", c)
	}

	replier.err = assistantsvc.ErrAssistantNotAnswer
	if rec := post("/api/spaces/"+commentSpace+"/issues/i_1/requester-replies", "u_member"); rec.Code != http.StatusConflict {
		t.Errorf("paused = %d", rec.Code)
	}
	if rec := post("/api/spaces/"+commentSpace+"/issues/i_far/requester-replies", "u_owner"); rec.Code != http.StatusNotFound {
		t.Errorf("other space's issue = %d", rec.Code)
	}
	if rec := post("/api/spaces/"+commentSpace+"/issues/i_1/requester-replies", "u_stranger"); rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Errorf("non-member = %d", rec.Code)
	}
	if len(replier.cmds) != 2 {
		t.Errorf("replier called %d times, want 2", len(replier.cmds))
	}
}
