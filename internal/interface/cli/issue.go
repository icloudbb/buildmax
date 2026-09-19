package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/interface/client"
)

func newIssueCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue",
		Short: "See the space work you own",
		Long: "Receive space work from the BuildMax server you are signed in to,\n" +
			"do it here, and say where it got to.\n\n" +
			"That is the whole scope: list what you were given, read one, work it\n" +
			"with `buildmax issue start`, and move its status when you are done. The\n" +
			"board, the workflow editor, and everything about who owns what stay in\n" +
			"Portal. Sign in with `buildmax login` first.",
	}
	cmd.AddCommand(newIssueListCommand())
	cmd.AddCommand(newIssueShowCommand())
	cmd.AddCommand(newIssueStartCommand())
	cmd.AddCommand(newIssueStatusCommand())
	cmd.AddCommand(newIssueCommentCommand())
	return cmd
}

func newIssueCommentCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment <issue-id>",
		Short: "Post a report on an issue",
		Long: "Posts one comment on an issue you can reach, signed in as you.\n\n" +
			"This is the report path a command can reach: an agent working here can\n" +
			"run it to say what happened, the same statement the in-process report\n" +
			"tool makes. The comment is recorded as a local agent report. Status,\n" +
			"owner, and sub-issues stay yours to change with `buildmax issue status`.\n\n" +
			"Give the body with -m, or leave it off to read the body from stdin.",
		Args: cobra.ExactArgs(1),
		RunE: runIssueComment,
	}
	cmd.Flags().StringP("message", "m", "", "the comment body; read from stdin when omitted")
	return cmd
}

func runIssueComment(cmd *cobra.Command, args []string) error {
	body, _ := cmd.Flags().GetString("message")
	if strings.TrimSpace(body) == "" {
		read, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("read comment body from stdin: %w", err)
		}
		body = string(read)
	}
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("empty comment: give a body with -m or on stdin")
	}
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	c := client.NewClient(serverURL)
	space, issue, err := c.FindIssue(cmd.Context(), token, args[0])
	if err != nil {
		return err
	}
	if err := c.CommentOnIssue(cmd.Context(), token, space.ID, issue.ID, body); err != nil {
		return fmt.Errorf("post comment: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Commented on %s.\n", issue.ID)
	return nil
}

func newIssueStartCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <issue-id>",
		Short: "Work an issue in this session: the agent can read it and report back",
		Long: "Scopes one local session to one issue and launches it, in the TUI or,\n" +
			"with -p, one print-mode run. The agent can read that issue and post a\n" +
			"report on it; its status, owner, executor, and sub-issues stay yours to\n" +
			"change.\n\n" +
			"This takes the same run flags as `buildmax` itself (-p, -r, --model,\n" +
			"--workspace, and so on). Requires login. The scope lasts for this\n" +
			"session only; it is not remembered.",
		Args: cobra.ExactArgs(1),
		RunE: runIssueStart,
	}
	addRunFlags(cmd)
	return cmd
}

func runIssueStart(cmd *cobra.Command, args []string) error {
	session, err := auth.OpenIssueSession(cmd.Context(), args[0])
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), err.Error())
		return &ExitError{Code: ExitUsage, Err: err}
	}
	return runAgentSession(cmd, session)
}

func newIssueListCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the issues you own, across every space you are in",
		RunE:  runIssueList,
	}
	cmd.Flags().String("status", "", "only issues with this status: todo, in_progress, or done")
	cmd.Flags().Int("limit", 50, "most issues to list per space")
	return cmd
}

func runIssueList(cmd *cobra.Command, _ []string) error {
	info, err := auth.Info()
	if err != nil {
		return fmt.Errorf("read credentials: %w", err)
	}
	if !info.LoggedIn || info.ServerURL == "" {
		return fmt.Errorf("not signed in: run `buildmax login` to see the work you own")
	}
	status, _ := cmd.Flags().GetString("status")
	if status != "" && !isKnownIssueStatus(status) {
		return fmt.Errorf("unknown status %q: use todo, in_progress, or done", status)
	}
	limit, _ := cmd.Flags().GetInt("limit")

	token, err := auth.TokenForServer(info.ServerURL)
	if err != nil {
		return fmt.Errorf("authenticate to %s: %w", info.ServerURL, err)
	}
	issues, problems := client.NewClient(info.ServerURL).ListOwnedIssues(cmd.Context(), token, status, limit)
	// Problems are printed before the list rather than swallowed: an inbox that
	// quietly omits a space is worse than one that says which space it could not
	// read.
	for _, problem := range problems {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v\n", problem)
	}
	if len(issues) == 0 {
		if len(problems) > 0 {
			return fmt.Errorf("no issues could be read")
		}
		fmt.Fprintln(cmd.OutOrStdout(), "You own nothing yet.")
		return nil
	}
	printOwnedIssues(cmd, issues)
	return nil
}

func printOwnedIssues(cmd *cobra.Command, issues []client.OwnedIssue) {
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ISSUE\tSTATUS\tSPACE\tTITLE")
	for _, item := range issues {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.Issue.ID, item.Issue.Status, item.SpaceName, oneLine(item.Issue.Title))
	}
	_ = w.Flush()
}

// oneLine keeps a multi-line title from breaking the table. Truncation is the
// table's job; the whole title is in Portal.
func oneLine(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " "))
	const width = 72
	if len([]rune(s)) <= width {
		return s
	}
	return string([]rune(s)[:width-1]) + "…"
}

func isKnownIssueStatus(status string) bool {
	switch status {
	case coreissue.StatusTodo, coreissue.StatusInProgress, coreissue.StatusDone:
		return true
	}
	return false
}

// issueSessionNotice says what a session working an Issue is about to do with
// space data, before it does any of it.
//
// The proposal's rule is that the server, the space, the Issue, and where the
// model sends prompts are visible before work crosses a boundary — not
// reconstructable afterwards from a tool call. A person who did not intend to
// hand a space's issue to a personal model should learn that here.
func issueSessionNotice(session *auth.IssueSession, source auth.ModelSource) string {
	if session == nil {
		return ""
	}
	space := session.SpaceName
	if space == "" {
		space = session.SpaceID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Working issue %s — %s (%s) in space %s on %s.\n",
		session.Issue.ID, oneLine(session.Issue.Title), session.Issue.Status, space, session.ServerURL)
	b.WriteString("The agent can read that issue and post a report on it. Its status, owner,\n" +
		"executor, and sub-issues stay yours to change.\n")
	if source.ServerURL != "" {
		fmt.Fprintf(&b, "Prompts go to %s.\n", source.ServerURL)
	} else {
		b.WriteString("Prompts go straight from this machine to whichever provider your model entry names; `buildmax models` shows which.\n")
	}
	return b.String()
}

func newIssueShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <issue-id>",
		Short: "Show one issue: what it asks for, how it was split up, and what has been said",
		Args:  cobra.ExactArgs(1),
		RunE:  runIssueShow,
	}
}

func newIssueStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status <issue-id> <todo|in_progress|done>",
		Short: "Move an issue's status",
		Long: "Moves an issue's status on the server.\n\n" +
			"This is a person's action on purpose. Status is what the space reads to\n" +
			"plan around, and `done` means someone accepted the work — so an agent\n" +
			"working the issue can say it believes the work is finished, and you\n" +
			"decide.\n\n" +
			"The change carries the version the issue was read at. If someone else\n" +
			"changed it in between, this refuses rather than overwriting them.",
		Args: cobra.ExactArgs(2),
		RunE: runIssueStatus,
	}
}

// signedInServer resolves the signed-in server and a token for it. Every
// server command needs the same two things and fails the same three ways.
func signedInServer(cmd *cobra.Command) (serverURL, token string, err error) {
	info, err := auth.Info()
	if err != nil {
		return "", "", fmt.Errorf("read credentials: %w", err)
	}
	if !info.LoggedIn || info.ServerURL == "" {
		return "", "", fmt.Errorf("not signed in: run `buildmax login` first")
	}
	token, err = auth.TokenForServer(info.ServerURL)
	if err != nil {
		return "", "", fmt.Errorf("authenticate to %s: %w", info.ServerURL, err)
	}
	return info.ServerURL, token, nil
}

func runIssueShow(cmd *cobra.Command, args []string) error {
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	c := client.NewClient(serverURL)
	space, issue, err := c.FindIssue(cmd.Context(), token, args[0])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "%s  %s\n", issue.ID, issue.Title)
	fmt.Fprintf(out, "%s in space %s\n", issue.Status, space.Name)
	if issue.OwnerID != nil {
		fmt.Fprintf(out, "owner %s\n", *issue.OwnerID)
	}
	if issue.ExecutorKind != nil && issue.ExecutorID != nil {
		fmt.Fprintf(out, "executor %s %s\n", *issue.ExecutorKind, *issue.ExecutorID)
	}
	if strings.TrimSpace(issue.Description) != "" {
		fmt.Fprintf(out, "\n%s\n", strings.TrimSpace(issue.Description))
	}
	children, comments, omitted, err := c.IssueThread(cmd.Context(), token, space.ID, issue.ID)
	if err != nil {
		// The issue itself printed. Saying the rest could not be read beats
		// showing an issue that looks like it has no discussion.
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not read sub-issues or discussion: %v\n", err)
		return nil
	}
	if len(children) > 0 {
		fmt.Fprintln(out, "\nSub-issues:")
		for _, child := range children {
			fmt.Fprintf(out, "  %s  %-12s %s\n", child.ID, child.Status, oneLine(child.Title))
		}
	}
	if omitted > 0 {
		fmt.Fprintf(out, "\nDiscussion (%d older not shown):\n", omitted)
	} else if len(comments) > 0 {
		fmt.Fprintln(out, "\nDiscussion:")
	}
	for _, comment := range comments {
		fmt.Fprintf(out, "\n  %s — %s\n", commentAuthorLabel(comment), comment.CreatedAt.Local().Format("2006-01-02 15:04"))
		for _, line := range strings.Split(strings.TrimSpace(comment.Body), "\n") {
			fmt.Fprintf(out, "    %s\n", line)
		}
	}
	fmt.Fprintf(out, "\nWork on it here: buildmax issue start %s\n", issue.ID)
	return nil
}

// commentAuthorLabel names who is speaking, and keeps a report from a machine
// nobody scheduled distinct from a run this deployment recorded.
func commentAuthorLabel(comment coreissue.Comment) string {
	switch comment.AuthorKind {
	case coreissue.CommentAuthorAgent:
		return "agent (ran on the server)"
	case coreissue.CommentAuthorLocalAgent:
		return "agent (ran locally, reported by " + comment.AuthorID + ")"
	case coreissue.CommentAuthorSystem:
		return "BuildMax"
	default:
		return comment.AuthorID
	}
}

func runIssueStatus(cmd *cobra.Command, args []string) error {
	issueID, status := args[0], args[1]
	if !isKnownIssueStatus(status) {
		return fmt.Errorf("unknown status %q: use todo, in_progress, or done", status)
	}
	serverURL, token, err := signedInServer(cmd)
	if err != nil {
		return err
	}
	c := client.NewClient(serverURL)
	space, issue, err := c.FindIssue(cmd.Context(), token, issueID)
	if err != nil {
		return err
	}
	if issue.Status == status {
		fmt.Fprintf(cmd.OutOrStdout(), "%s is already %s.\n", issue.ID, status)
		return nil
	}
	updated, err := c.SetIssueStatus(cmd.Context(), token, space.ID, issue.ID, status, issue.Version)
	if err != nil {
		return fmt.Errorf("set status: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", updated.ID, issue.Status, updated.Status)
	return nil
}
