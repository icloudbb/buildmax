// Package pluginmgr installs plugins from a deployment's Marketplace.
//
// It is the mechanism both local surfaces share: the CLI and Desktop each own
// how they ask and what they print, and neither owns staging, digest
// verification, or the rename that puts a release in place. A second copy of
// that would be a second set of rules about when a half-installed plugin is
// visible.
package pluginmgr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/infra/git"
	archive "github.com/icloudbb/buildmax/internal/infra/pluginarchive"
	"github.com/icloudbb/buildmax/internal/infra/pluginwire"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/interface/client"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	inspect "github.com/icloudbb/buildmax/internal/service/plugininspect"
)

// Reserved directories inside the plugins directory. Dot-prefixed so discovery
// skips them, which is what lets a half-finished install sit beside working
// plugins without becoming one.
const (
	stagingPrefix = ".staging-"
	retiredPrefix = ".retired-"
)

// ErrNotSignedIn means there is no login to reach a Marketplace with.
var ErrNotSignedIn = errors.New("not signed in: run `buildmax login` first")

// ErrIsCheckout means the installed copy is a Git working tree, which may hold
// work that exists nowhere else. How a surface offers to override that is the
// surface's own word — a flag here, a confirmation there — so the message says
// what is true and leaves the wording to the caller.
var ErrIsCheckout = errors.New("the installed plugin is a Git checkout")

// Session is an authenticated connection to one deployment's Marketplace.
type Session struct {
	client    *client.Client
	token     string
	serverURL string
}

// Open reads the stored login and prepares a session.
func Open() (*Session, error) {
	info, err := auth.Info()
	if err != nil {
		return nil, fmt.Errorf("load auth: %w", err)
	}
	if !info.LoggedIn {
		return nil, ErrNotSignedIn
	}
	token, err := auth.TokenForServer(info.ServerURL)
	if err != nil {
		return nil, err
	}
	return &Session{
		client: auth.ServerClient(info.ServerURL), token: token, serverURL: info.ServerURL,
	}, nil
}

// ServerURL is the deployment this session talks to.
func (s *Session) ServerURL() string { return s.serverURL }

// Options describes which release a caller wants installed.
type Options struct {
	Name string
	// Version asks for one exact release. Empty takes the default selection.
	Version string
	// AllowYanked permits a withdrawn release, which a recovery has to say.
	AllowYanked bool
	// RequireInstalled refuses when the plugin is not there yet, which is what
	// separates an update from an install.
	RequireInstalled bool
}

// Plan is what an install would do, resolved before anything is downloaded.
//
// It exists so a surface can show what is about to change while the decision
// is still open, which is the moment the capability report is worth reading.
type Plan struct {
	Release coreplugin.Release
	// AlreadyInstalled means this exact release is the one already in place.
	AlreadyInstalled bool
}

// Resolve picks the release to install without installing it.
func (s *Session) Resolve(ctx context.Context, opts Options) (Plan, error) {
	existing, installed, err := existingInstall(opts.Name)
	if err != nil {
		return Plan{}, err
	}
	if opts.RequireInstalled && !installed {
		return Plan{}, fmt.Errorf("%s is not installed", opts.Name)
	}
	if installed {
		if err := checkReplaceable(existing); err != nil {
			return Plan{}, err
		}
	}

	detail, err := s.client.GetPlugin(ctx, s.token, opts.Name)
	if err != nil {
		return Plan{}, err
	}
	release, err := pluginsvc.SelectRelease(detail.Releases, pluginsvc.SelectOptions{
		Version:       opts.Version,
		ClientVersion: config.Version,
		AllowYanked:   opts.AllowYanked,
		// Naming a prerelease exactly works; taking one by default does not.
		AllowPrerelease: opts.Version != "",
	})
	if err != nil {
		return Plan{}, err
	}
	already := installed &&
		existing.State.ReleaseVersion == release.Version &&
		existing.State.Digest == release.Digest
	return Plan{Release: *release, AlreadyInstalled: already}, nil
}

// Install downloads a resolved release and puts it in place.
func (s *Session) Install(ctx context.Context, opts Options, release coreplugin.Release) error {
	pluginsDir := config.PluginsDir()
	staged, err := s.stage(ctx, opts, release)
	if err != nil {
		return err
	}
	defer os.RemoveAll(staged.root)

	if err := swapIn(staged.extracted, filepath.Join(pluginsDir, opts.Name)); err != nil {
		return err
	}
	return recordInstalled(pluginsDir, opts.Name, s.serverURL, release)
}

// Publish packs a directory and uploads it.
func (s *Session) Publish(ctx context.Context, dir string) (*coreplugin.Release, error) {
	pkg, err := Inspect(dir)
	if err != nil {
		return nil, err
	}
	if errs := coreplugin.Errors(pkg.Findings); len(errs) > 0 {
		return nil, fmt.Errorf("%s would not load: %s", dir, errs[0].String())
	}
	if pkg.Manifest.Version == "" {
		return nil, fmt.Errorf("%s has no version, which publishing requires", coreplugin.ManifestFile)
	}

	body, cleanup, _, err := Pack(dir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return s.client.PublishRelease(ctx, s.token, pkg.Manifest.Name, body, ReadSourceClaim(ctx, dir))
}

// Inspect reads a plugin directory the way a run would.
func Inspect(dir string) (inspect.Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return inspect.Package{}, err
	}
	return inspect.Dir(os.DirFS(abs))
}

// Pack writes the archive to a temporary file rather than a buffer, so
// publishing a large plugin does not hold it twice in memory.
func Pack(dir string) (io.Reader, func(), int64, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, nil, 0, err
	}
	f, err := os.CreateTemp("", "buildmax-publish-*.tar.gz")
	if err != nil {
		return nil, nil, 0, err
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	sum, err := archive.Pack(f, os.DirFS(abs), archive.Limits{})
	if err != nil {
		cleanup()
		return nil, nil, 0, fmt.Errorf("pack %s: %w", abs, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		cleanup()
		return nil, nil, 0, err
	}
	return f, cleanup, sum.Bytes, nil
}

// ReadSourceClaim describes the checkout a package was packed from.
//
// A directory that is not a repository produces an empty claim, which is not an
// error: a package assembled by hand is a legitimate thing to publish, and the
// catalog says so rather than inventing provenance.
func ReadSourceClaim(ctx context.Context, dir string) coreplugin.ReleaseSource {
	if !git.IsRepository(dir) {
		return coreplugin.ReleaseSource{}
	}
	claim := coreplugin.ReleaseSource{RemoteURL: git.ReadRemoteURL(ctx, dir)}
	if st, err := git.ReadStatus(ctx, dir); err == nil {
		claim.Commit, claim.Branch, claim.Dirty = st.Commit, st.Branch, st.Dirty
	}
	return claim
}

// Uninstall removes an installed plugin and returns the path it removed.
func Uninstall(name string, force bool) (string, error) {
	discovery := config.DiscoverPlugins()
	found, ok := findPlugin(discovery, name)
	if !ok {
		return "", fmt.Errorf("no plugin named %q under %s", name, discovery.Dir)
	}
	// A checkout may hold work that exists nowhere else, and uninstall is not
	// the command anybody expects to lose it to.
	if found.Source() == config.PluginSourceRepository && !force {
		return "", fmt.Errorf("%w: %s is at %s", ErrIsCheckout, found.Name(), found.Path)
	}
	if err := os.RemoveAll(found.Path); err != nil {
		return "", err
	}
	err := config.UpdatePluginStates(discovery.Dir, func(s *config.PluginStates) error {
		s.Remove(found.Dir)
		return nil
	})
	return found.Path, err
}

// SetDisabled stops a plugin loading, or lets it load again.
//
// Only the flag is written. Recording the source classification here would
// freeze an answer the directory can change afterwards.
func SetDisabled(name string, disabled bool) (string, error) {
	discovery := config.DiscoverPlugins()
	found, ok := findPlugin(discovery, name)
	if !ok {
		return "", fmt.Errorf("no plugin named %q under %s", name, discovery.Dir)
	}
	err := config.UpdatePluginStates(discovery.Dir, func(s *config.PluginStates) error {
		st, _ := s.Get(found.Dir)
		st.Disabled = disabled
		s.Set(found.Dir, st)
		return nil
	})
	return found.Name(), err
}

func findPlugin(d config.PluginDiscovery, name string) (config.DiscoveredPlugin, bool) {
	for _, p := range d.Plugins {
		if p.Name() == name || p.Dir == name {
			return p, true
		}
	}
	return config.DiscoveredPlugin{}, false
}

func existingInstall(name string) (config.DiscoveredPlugin, bool, error) {
	found, ok := findPlugin(config.DiscoverPlugins(), name)
	return found, ok, nil
}

// checkReplaceable refuses to overwrite something BuildMax did not put there.
func checkReplaceable(existing config.DiscoveredPlugin) error {
	switch existing.Source() {
	case config.PluginSourceRepository:
		return fmt.Errorf("%w: %s is at %s, and installing would replace it. "+
			"Remove or rename it first if that is what you want",
			ErrIsCheckout, existing.Name(), existing.Path)
	case config.PluginSourceMarketplace:
		return nil
	default:
		return fmt.Errorf("%s at %s was not installed from the Marketplace: "+
			"remove it first if you want the published release instead",
			existing.Name(), existing.Path)
	}
}

// staged is one download on its way in.
type staged struct {
	root      string
	extracted string
}

func (s *Session) stage(ctx context.Context, opts Options, release coreplugin.Release) (staged, error) {
	pluginsDir := config.PluginsDir()
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return staged{}, err
	}
	root, err := os.MkdirTemp(pluginsDir, stagingPrefix)
	if err != nil {
		return staged{}, err
	}
	out := staged{root: root, extracted: filepath.Join(root, "extract")}

	archivePath := filepath.Join(root, "package.tar.gz")
	f, err := os.Create(archivePath)
	if err != nil {
		os.RemoveAll(root)
		return staged{}, err
	}
	servedDigest, err := s.client.DownloadRelease(
		ctx, s.token, opts.Name, release.Version, opts.AllowYanked, f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.RemoveAll(root)
		return staged{}, err
	}
	// Two checks, not one. The header says what the server thinks it sent; the
	// catalog record says what was published. A download is only right when it
	// matches both, and comparing the bytes to the record is what catches a
	// truncation the header would have described correctly.
	if servedDigest != "" && servedDigest != release.Digest {
		os.RemoveAll(root)
		return staged{}, fmt.Errorf("the server sent digest %s for %s %s, which the catalog records as %s",
			servedDigest, opts.Name, release.Version, release.Digest)
	}
	if err := verifyDigest(archivePath, release.Digest); err != nil {
		os.RemoveAll(root)
		return staged{}, err
	}
	if err := extractAndValidate(archivePath, out.extracted, opts.Name); err != nil {
		os.RemoveAll(root)
		return staged{}, err
	}
	return out, nil
}

func verifyDigest(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := archive.VerifyDigest(f, want); err != nil {
		return fmt.Errorf("the downloaded package does not match what was published: %w", err)
	}
	return nil
}

// extractAndValidate unpacks a verified package and reads it the way a run
// would, so a package that would not load never reaches the plugins directory.
func extractAndValidate(archivePath, dest, name string) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := archive.Extract(f, dest, archive.Limits{}); err != nil {
		return fmt.Errorf("unpack: %w", err)
	}
	pkg, err := inspect.Dir(os.DirFS(dest))
	if err != nil {
		return err
	}
	if errs := coreplugin.Errors(pkg.Findings); len(errs) > 0 {
		return fmt.Errorf("the downloaded package would not load: %s", errs[0].String())
	}
	if pkg.Manifest.Name != name {
		return fmt.Errorf("the downloaded package names %q, not %q", pkg.Manifest.Name, name)
	}
	return nil
}

// swapIn exchanges the staged directory with the active one.
//
// The old directory is moved aside before the new one lands and removed only
// once it has, so an interrupted install leaves either the previous plugin or
// the new one — never half of each.
func swapIn(stagedDir, active string) error {
	retired := ""
	if _, err := os.Stat(active); err == nil {
		retired = filepath.Join(filepath.Dir(active), retiredPrefix+filepath.Base(active))
		_ = os.RemoveAll(retired)
		if err := os.Rename(active, retired); err != nil {
			return fmt.Errorf("set aside the installed copy: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Rename(stagedDir, active); err != nil {
		if retired != "" {
			// Put back what was working before reporting the failure.
			_ = os.Rename(retired, active)
		}
		return fmt.Errorf("install: %w", err)
	}
	if retired != "" {
		// Nothing is kept for rollback: an exact release can always be
		// downloaded again, and a second copy on disk raises a question about
		// which one is current that nothing here would answer.
		_ = os.RemoveAll(retired)
	}
	return nil
}

func recordInstalled(pluginsDir, name, serverURL string, release coreplugin.Release) error {
	now := time.Now().UTC()
	return config.UpdatePluginStates(pluginsDir, func(s *config.PluginStates) error {
		st, _ := s.Get(name)
		st.Source = config.PluginSourceMarketplace
		st.MarketplaceServer = serverURL
		st.CatalogID = release.PluginName
		st.ReleaseVersion = release.Version
		st.Digest = release.Digest
		// Repository fields would describe a checkout this copy is not.
		st.RepositoryURL, st.LastCommit = "", ""
		if st.InstalledAt.IsZero() {
			st.InstalledAt = now
		}
		st.UpdatedAt = now
		s.Set(name, st)
		return nil
	})
}

// ListActivations reads what a space has activated for its background runs.
//
// It sits here rather than in the CLI so Desktop can show the same thing: a
// space's activations are a property of the deployment, not of this machine.
func (s *Session) ListActivations(ctx context.Context, spaceID string) (*pluginwire.ActivationsResponse, error) {
	return s.client.ListSpaceActivations(ctx, s.token, spaceID)
}
