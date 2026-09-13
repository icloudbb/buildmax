package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/localproject"
	"github.com/icloudbb/buildmax/internal/core/session"
	"github.com/icloudbb/buildmax/internal/util"
)

// sessionTarget is what a run continues and where it runs.
//
// The two travel together because resuming decides both: a session belongs to
// one Project, and continuing it somewhere that resolves to a different Project
// is not the same conversation carried on, it is a conversation moved.
type sessionTarget struct {
	SessionID string
	Workspace string
	// CreateIfMissing is set only for an explicit --session-id, whose contract
	// is "load if exists, else create". -r/--resume and --continue leave it
	// false: they continue a session that already exists and must keep erroring
	// on an unknown id.
	CreateIfMissing bool
}

// resolveSessionTarget settles --continue and --resume against the Workspace
// this run is in and the local Project that owns it.
//
// --continue means the newest session recorded in *this* Workspace. Project is
// the scope of shared memory and deliberately not the scope of resume: memory
// asks what is true about this repository, resume asks which conversation the
// user was just having. The two coincide in a single checkout and diverge
// exactly where worktrees are used, and continuing at Project scope would let
// `buildmax -c` in one worktree pick a session recorded in a sibling and
// execute there — moving the working root out from under the one workflow
// whose whole purpose is branch isolation.
//
// --resume stays a global lookup by id, with the Project recorded on the
// session authoritative about where it may be continued. See
// docs/design/local-project-memory.md §11.2.
func resolveSessionTarget(ctx context.Context, resumeID string, cont, acrossProject bool, workspace string, workspaceGiven bool) (sessionTarget, error) {
	target := sessionTarget{SessionID: resumeID, Workspace: workspace}
	switch {
	case resumeID != "":
		return resolveResumeTarget(ctx, target, workspaceGiven)
	case cont:
		return resolveContinueTarget(ctx, target, acrossProject)
	default:
		return target, nil
	}
}

// resolveContinueTarget picks the newest session recorded in this Workspace,
// widening to the Project only when the user asked for it.
func resolveContinueTarget(ctx context.Context, target sessionTarget, acrossProject bool) (sessionTarget, error) {
	root, err := util.ResolveWorkspaceRoot(target.Workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot resolve the workspace: %v\n", err)
		return sessionTarget{}, err
	}
	project, err := currentProject(ctx, root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot tell which project this directory belongs to: %v\n", err)
		return sessionTarget{}, err
	}

	list, err := agentapp.NewSessionManager(config.SessionsDir()).List()
	if err != nil {
		slog.Error("load session list failed", "err", err)
		return sessionTarget{}, fmt.Errorf("load session list: %w", err)
	}
	inProject := filterByProject(list, project.ID)

	if !acrossProject {
		if last := latestSessionItem(filterByWorkspace(inProject, root)); last != nil {
			slog.Info("continue with last session", "id", last.ID, "workspace", root)
			target.SessionID = last.ID
			return target, nil
		}
		// Not silently borrowed from a sibling Workspace. Widening is the
		// user's decision because it changes the directory the turn runs in,
		// and it is named here so the recovery path is findable.
		if elsewhere := len(inProject); elsewhere > 0 {
			fmt.Fprintf(os.Stderr,
				"no sessions yet in %s.\n%s has %d session(s) in other directories; "+
					"continue the newest of those with `--continue --project`.\n",
				root, project.Name, elsewhere)
			return sessionTarget{}, fmt.Errorf("no sessions to continue in %s", root)
		}
		fmt.Fprintf(os.Stderr, "no sessions yet in %s; start one with -p PROMPT or the TUI\n", root)
		return sessionTarget{}, fmt.Errorf("no sessions to continue in %s", root)
	}

	last := latestSessionItem(inProject)
	if last == nil {
		fmt.Fprintf(os.Stderr, "no sessions yet in %s; start one with -p PROMPT or the TUI\n", project.Name)
		return sessionTarget{}, fmt.Errorf("no sessions to continue in project %s", project.ID)
	}
	if last.Workspace != "" && !sameWorkspace(last.Workspace, root) {
		// A root change is never implicit: the directory the turn will run in
		// is printed before the first turn, not discovered from its output.
		fmt.Fprintf(os.Stderr, "continuing in %s\n", last.Workspace)
		target.Workspace = last.Workspace
	}
	slog.Info("continue with last session", "id", last.ID, "project_id", project.ID, "across_project", true)
	target.SessionID = last.ID
	return target, nil
}

// resolveResumeTarget checks that an explicitly named session may be continued
// here, and picks the directory to continue it in.
func resolveResumeTarget(ctx context.Context, target sessionTarget, workspaceGiven bool) (sessionTarget, error) {
	loaded, err := agentapp.NewSessionManager(config.SessionsDir()).Load(target.SessionID, session.LoadMetaOnly)
	if err != nil {
		// Not this function's refusal to make: an unreadable or unknown session
		// is reported where the session is opened, with the message that path
		// already gives. Resuming proceeds and fails there.
		slog.Warn("resume: could not read session metadata", "id", target.SessionID, "err", err)
		return target, nil
	}
	// A session from before Projects existed, or one a worker wrote, belongs to
	// no local Project. Inferring one from the directory it is being resumed in
	// would attach it -- and later, another Project's memory -- by path
	// coincidence, which is the failure this design exists to remove.
	if loaded.Meta.ProjectID == "" {
		return target, nil
	}

	if !workspaceGiven && loaded.Meta.Workspace != "" {
		if info, statErr := os.Stat(loaded.Meta.Workspace); statErr == nil && info.IsDir() {
			// Resume where the conversation was, rather than wherever the
			// terminal happens to be. Nothing needs re-checking: the recorded
			// root is what produced this session's Project.
			target.Workspace = loaded.Meta.Workspace
			return target, nil
		}
	}

	here, err := currentProject(ctx, target.Workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot tell which project this directory belongs to: %v\n", err)
		return sessionTarget{}, err
	}
	if here.ID == loaded.Meta.ProjectID {
		return target, nil
	}

	// Refused rather than allowed with a warning: the session would go on to
	// read this Project's memory and record its work here, and neither is
	// undone by noticing afterwards.
	owner := projectLabel(ctx, loaded.Meta.ProjectID)
	err = fmt.Errorf("session %s belongs to project %s, not %s (%s)",
		target.SessionID, owner, here.Name, here.ID)
	fmt.Fprintf(os.Stderr, "%v\n\nResume it from a directory of that project, or start a new session here.\n", err)
	return sessionTarget{}, err
}

// projectLabel names a Project for a message. A Project that cannot be read is
// named by its id alone: the point of the message is to tell two Projects
// apart, and that still does.
func projectLabel(ctx context.Context, id string) string {
	p, err := agentapp.NewProjectManager(config.ProjectsDir()).Store().Get(ctx, id)
	if err != nil || p.Name == "" {
		if errors.Is(err, localproject.ErrNotFound) {
			return id + " (no longer on this machine)"
		}
		return id
	}
	return fmt.Sprintf("%s (%s)", p.Name, id)
}

// filterByProject keeps the sessions belonging to projectID. A projectless
// session never matches: it is not evidence of belonging anywhere, and treating
// it as a match would put a worker's or a pre-Project session into a picker
// scoped to this repository.
func filterByProject(entries []session.ItemSummary, projectID string) []session.ItemSummary {
	if projectID == "" {
		return nil
	}
	out := make([]session.ItemSummary, 0, len(entries))
	for _, e := range entries {
		if e.ProjectID == projectID {
			out = append(out, e)
		}
	}
	return out
}

// filterByWorkspace keeps the sessions recorded in root. A session that never
// recorded a root never matches: it is not evidence of having run here.
func filterByWorkspace(entries []session.ItemSummary, root string) []session.ItemSummary {
	out := make([]session.ItemSummary, 0, len(entries))
	for _, e := range entries {
		if e.Workspace != "" && sameWorkspace(e.Workspace, root) {
			out = append(out, e)
		}
	}
	return out
}

// sameWorkspace compares two recorded roots. Sessions store whichever spelling
// the runtime selected, so the comparison resolves symlinks on both sides for
// the same reason Project resolution does — one directory reached two ways is
// one directory.
func sameWorkspace(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, aerr := filepath.EvalSymlinks(a)
	rb, berr := filepath.EvalSymlinks(b)
	return aerr == nil && berr == nil && filepath.Clean(ra) == filepath.Clean(rb)
}

func latestSessionItem(entries []session.ItemSummary) *session.ItemSummary {
	var best *session.ItemSummary
	for i := range entries {
		if best == nil || entries[i].CreatedAt.After(best.CreatedAt) {
			best = &entries[i]
		}
	}
	return best
}
