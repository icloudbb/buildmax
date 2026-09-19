package tool

import (
	"context"
	"time"
)

// IssueSnapshot is the bounded view of the one Issue a run is working.
type IssueSnapshot struct {
	Title        string
	Description  string
	Status       string
	ExecutorKind string
	Children     []IssueChild
	Comments     []IssueComment
	// OmittedComments is how many older comments the window left out, so the
	// reader can say the thread is longer than what it read instead of assuming
	// it saw all of it.
	OmittedComments int
}

// IssueChild is one sub-issue, as title and status. Not the child's own
// description: a parent's agent needs to know what was split out, not to read
// the whole subtree into its context.
type IssueChild struct {
	Title  string
	Status string
}

// IssueComment is one statement on the thread. AuthorKind is carried because a
// reader that cannot tell a spacemate's comment from its own principal's
// instruction has no basis for treating them differently.
type IssueComment struct {
	AuthorKind string
	Body       string
	CreatedAt  time.Time
}

// IssueReport is one statement an agent makes about the work it did.
type IssueReport struct {
	Body string
	// ArtifactIDs names artifacts the run already published. Naming, not
	// attaching: the artifact exists under its own identity, and repeating an
	// object-store path in a comment would create a second, weaker reference.
	ArtifactIDs []string
}

// IssueClient reads and reports on the one Issue a run is working.
//
// A port rather than the capability itself: a caller must not learn whether the
// call reaches a server over a run token, a person's session, or an in-process
// service, and must not grow a dependency on the issue service to find out. The
// `buildmax issue` command uses it to talk to the worker route through the run
// bridge; see docs/design/agent-bridge-cli.md.
//
// There is no issue identifier in either method. The scope is fixed when the
// client is constructed, so a second issue cannot be addressed through it.
type IssueClient interface {
	Issue(ctx context.Context) (IssueSnapshot, error)
	Report(ctx context.Context, in IssueReport) error
}
