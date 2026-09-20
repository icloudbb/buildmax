package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/icloudbb/buildmax/internal/config"
	launchstore "github.com/icloudbb/buildmax/internal/infra/locallaunchpadstore"
	"github.com/icloudbb/buildmax/internal/util"
)

// LaunchpadEntryPayload is one launchpad entry as the frontend sees it.
type LaunchpadEntryPayload struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Target string   `json:"target"`
	Args   []string `json:"args,omitempty"`
}

func launchpadEntryPayload(r launchstore.Record) LaunchpadEntryPayload {
	return LaunchpadEntryPayload{ID: r.ID, Name: r.Name, Target: r.Target, Args: r.Args}
}

// ensureLaunchpadStore lazily opens the launchpad store. It resolves the path
// only on first use so an App built in a test that never touches the launchpad
// does not require BUILDMAX_HOME.
func (a *App) ensureLaunchpadStore() *launchstore.FileStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.launchpad == nil {
		a.launchpad = launchstore.NewFileStore(config.LaunchpadPath())
	}
	return a.launchpad
}

// ListLaunchpadEntries returns every launchpad entry, oldest first.
func (a *App) ListLaunchpadEntries() ([]LaunchpadEntryPayload, error) {
	rows, err := a.ensureLaunchpadStore().List()
	if err != nil {
		return nil, err
	}
	out := make([]LaunchpadEntryPayload, len(rows))
	for i, r := range rows {
		out[i] = launchpadEntryPayload(r)
	}
	return out, nil
}

// PickLaunchpadTarget opens a native file picker for an application or
// executable and returns the selected path, or "" if the user cancelled. It is a
// separate call so the frontend can offer "add an application" without the user
// typing a path. On macOS it starts in /Applications, where app bundles live.
func (a *App) PickLaunchpadTarget() (string, error) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return "", fmt.Errorf("app not ready")
	}
	opts := wruntime.OpenDialogOptions{Title: "Select Application"}
	if runtime.GOOS == "darwin" {
		opts.DefaultDirectory = "/Applications"
	}
	return wruntime.OpenFileDialog(ctx, opts)
}

// AddLaunchpadEntry records a new quick-launch entry for target with optional
// arguments. An empty name is derived from the target's file name so a picked
// application already reads well.
func (a *App) AddLaunchpadEntry(name, target string, args []string) (LaunchpadEntryPayload, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return LaunchpadEntryPayload{}, fmt.Errorf("a target is required")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = deriveLaunchpadName(target)
	}
	id, err := util.NewPublicID()
	if err != nil {
		return LaunchpadEntryPayload{}, err
	}
	r := launchstore.Record{
		ID:        id,
		Name:      name,
		Target:    target,
		Args:      args,
		CreatedAt: time.Now().UTC(),
	}
	if err := a.ensureLaunchpadStore().Add(r); err != nil {
		return LaunchpadEntryPayload{}, err
	}
	return launchpadEntryPayload(r), nil
}

// RemoveLaunchpadEntry drops one entry.
func (a *App) RemoveLaunchpadEntry(id string) error {
	return a.ensureLaunchpadStore().Delete(id)
}

// LaunchEntry starts the application for the given entry and returns once it has
// been handed to the OS. It is fire-and-forget: the child is reaped in the
// background so it is not left a zombie, but the app's lifetime does not block
// the caller.
func (a *App) LaunchEntry(id string) error {
	rec, err := a.ensureLaunchpadStore().Get(id)
	if err != nil {
		return err
	}
	name, argv := launchArgv(runtime.GOOS, rec.Target, rec.Args)
	cmd := exec.Command(name, argv...)
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch %q: %w", rec.Name, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// deriveLaunchpadName turns a target path into a friendly default name: the file
// name with a leading directory and a trailing extension removed, so
// "/Applications/Visual Studio Code.app" becomes "Visual Studio Code" and
// "/usr/bin/htop" becomes "htop". A URL or bare name is returned unchanged.
func deriveLaunchpadName(target string) string {
	base := filepath.Base(target)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	if base == "" || base == "." || base == string(filepath.Separator) {
		return target
	}
	return base
}

// launchArgv builds the program and arguments that start target on goos.
//
//   - macOS: `open` launches an application bundle, opens a document, or a URL
//     with the right handler; arguments for the launched app follow --args, which
//     also requires naming the app with -a.
//   - Windows: the shell's `start` handles bundles, documents, and URLs; the
//     empty title token guards against a quoted target being read as the window
//     title.
//   - Other (Linux): xdg-open cannot forward arguments, so an entry that carries
//     some is run directly; otherwise the target is handed to the default handler.
func launchArgv(goos, target string, args []string) (string, []string) {
	switch goos {
	case "darwin":
		if len(args) > 0 {
			return "open", append([]string{"-a", target, "--args"}, args...)
		}
		return "open", []string{target}
	case "windows":
		return "cmd", append([]string{"/c", "start", "", target}, args...)
	default:
		if len(args) > 0 {
			return target, args
		}
		return "xdg-open", []string{target}
	}
}
