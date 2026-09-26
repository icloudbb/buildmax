package auth

import (
	"context"
	"fmt"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
)

// IssueSession is one local session's link to one space Issue.
//
// It carries what the session must be able to say out loud: which server and
// space the work came from, and which Issue. Work crossing that boundary should
// be visible before it crosses, not inferable afterwards from a tool call. The
// Agent reads and reports on the Issue by running `buildmax issue <id>`, so the
// session holds no in-process client; it lasts one run.
type IssueSession struct {
	ServerURL string
	SpaceID   string
	SpaceName string
	Issue     coreissue.Issue
}

// OpenIssueSession scopes this session to one Issue on the server it is signed
// in to.
//
// Unlike the artifact capability, a failure here is returned rather than
// swallowed. Artifacts are resolved for every session and their absence just
// means the session has no server; an Issue is resolved only because someone
// asked for one by name, and starting anyway against no Issue would be
// answering a different request than the one made.
func OpenIssueSession(ctx context.Context, issueID string) (*IssueSession, error) {
	info, err := Info()
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	if !info.LoggedIn || info.ServerURL == "" {
		return nil, fmt.Errorf("not signed in: run `buildmax login` to work on a space issue")
	}
	token, err := TokenForServer(info.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("authenticate to %s: %w", info.ServerURL, err)
	}
	space, issue, err := ServerClient(info.ServerURL).FindIssue(ctx, token, issueID)
	if err != nil {
		return nil, err
	}
	return &IssueSession{
		ServerURL: info.ServerURL,
		SpaceID:   space.ID,
		SpaceName: space.Name,
		Issue:     issue,
	}, nil
}
