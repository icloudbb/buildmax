package desktop

import (
	"github.com/icloudbb/buildmax/internal/config"
	snapstore "github.com/icloudbb/buildmax/internal/infra/localterminalsnapshotstore"
)

// ensureTerminalSnapshotStore lazily opens the terminal snapshot store, resolving
// its path only on first use so a test that never touches it needs no
// BUILDMAX_HOME.
func (a *App) ensureTerminalSnapshotStore() *snapstore.FileStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.terminalSnapshots == nil {
		a.terminalSnapshots = snapstore.NewFileStore(config.TerminalSnapshotsPath())
	}
	return a.terminalSnapshots
}

// SaveTerminalSnapshot stores the serialized contents of one terminal tab, keyed
// by its project and the frontend's stable restore key, so a restart can restore
// what it showed.
func (a *App) SaveTerminalSnapshot(projectID, restoreKey, content string) error {
	return a.ensureTerminalSnapshotStore().Save(projectID, restoreKey, content)
}

// LoadTerminalSnapshot returns a terminal tab's last saved contents, or "" when
// none is stored.
func (a *App) LoadTerminalSnapshot(projectID, restoreKey string) (string, error) {
	return a.ensureTerminalSnapshotStore().Load(projectID, restoreKey)
}

// PruneTerminalSnapshots drops snapshots for a project whose restore key is not
// in keep, discarding the contents of terminals the user has closed.
func (a *App) PruneTerminalSnapshots(projectID string, keep []string) error {
	return a.ensureTerminalSnapshotStore().Prune(projectID, keep)
}
