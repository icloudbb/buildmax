import { useCallback, useEffect, useState } from 'react';
import { folderBaseName } from '../lib/format';

export function CreateProjectModal({ app, onCreate, onClose }) {
  const [name, setName] = useState('');
  const [folderPath, setFolderPath] = useState('');
  const [creating, setCreating] = useState(false);
  const [browseError, setBrowseError] = useState('');

  useEffect(() => {
    function onKey(e) { if (e.key === 'Escape') onClose(); }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  async function handleBrowse() {
    setBrowseError('');
    try {
      const path = await app.OpenFolderDialog();
      if (path) {
        setFolderPath(path);
        if (!name.trim()) {
          const base = folderBaseName(path);
          if (base) setName(base);
        }
      }
    } catch {
      setBrowseError('Could not open folder picker.');
    }
  }

  async function handleCreate() {
    const trimmedName = name.trim();
    if (!trimmedName || !folderPath || creating) return;
    setCreating(true);
    try {
      await onCreate(trimmedName, folderPath);
    } finally {
      setCreating(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={(e) => { if (e.target === e.currentTarget) onClose(); }} role="presentation">
      <div className="modal-panel" role="dialog" aria-modal="true" aria-label="New Project">
        <div className="modal-header">
          <h2 className="modal-title">New Project</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">×</button>
        </div>
        <div className="modal-body">
          <label className="modal-label" htmlFor="proj-name">Name</label>
          <input
            id="proj-name"
            className="modal-input"
            placeholder="My Project"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') folderPath ? handleCreate() : handleBrowse(); }}
            autoFocus
          />
          <label className="modal-label">Folder</label>
          <button type="button" className="modal-browse-btn" onClick={handleBrowse}>
            {folderPath
              ? <span className="modal-browse-path" title={folderPath}>{folderPath}</span>
              : <span className="modal-browse-placeholder">Choose folder…</span>}
          </button>
          {browseError && <p className="modal-field-error">{browseError}</p>}
        </div>
        <div className="modal-footer">
          <button type="button" className="modal-btn modal-btn--cancel" onClick={onClose}>Cancel</button>
          <button
            type="button"
            className="modal-btn modal-btn--primary"
            onClick={handleCreate}
            disabled={!name.trim() || !folderPath || creating}
          >
            {creating ? 'Creating…' : 'Create Project'}
          </button>
        </div>
      </div>
    </div>
  );
}

// --- Shared info modal ---

export function InfoModal({ title, onClose, children, className }) {
  useEffect(() => {
    function onKey(e) { if (e.key === 'Escape') onClose(); }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);
  return (
    <div
      className="modal-overlay"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
      role="presentation"
    >
      <div className={`modal-panel info-modal-panel${className ? ` ${className}` : ''}`} role="dialog" aria-modal="true" aria-label={title}>
        <div className="modal-header">
          <h2 className="modal-title">{title}</h2>
          <button type="button" className="modal-close" onClick={onClose} aria-label="Close">×</button>
        </div>
        <div className="info-modal-body">{children}</div>
      </div>
    </div>
  );
}

export function InfoList({ items, emptyText }) {
  if (!items) return <p className="info-modal__muted">Loading…</p>;
  if (!items.length) return <p className="info-modal__muted">{emptyText}</p>;
  return (
    <ul className="info-modal__list">
      {items.map((item, i) => (
        <li key={item.key ?? i} className={`info-modal__item ${item.active ? 'info-modal__item--active' : ''}`}>
          <div className="info-modal__item-row">
            <span className="info-modal__item-name">{item.name}</span>
            {item.badge && (
              <span className={`info-modal__badge info-modal__badge--${item.badgeVariant ?? 'default'}`}>
                {item.badge}
              </span>
            )}
          </div>
          {item.sub && <span className="info-modal__item-sub">{item.sub}</span>}
          {item.path && <span className="info-modal__item-path">{item.path}</span>}
        </li>
      ))}
    </ul>
  );
}

// --- MCP modal ---

export function MCPModal({ projectID, app, onClose }) {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    app.GetSlashMCP(projectID)
      .then(setResult)
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  let items = null;
  if (result) {
    if (result.load_error) {
      return (
        <InfoModal title="MCP Servers" onClose={onClose}>
          <p className="info-modal__error">{result.load_error}</p>
        </InfoModal>
      );
    }
    items = (result.servers ?? []).map((s) => ({
      key: s.id,
      name: s.id,
      badge: s.ok ? `${s.tool_count} tool${s.tool_count !== 1 ? 's' : ''}` : 'error',
      badgeVariant: s.ok ? 'ok' : 'err',
      sub: s.type + ((!s.ok && s.error) ? ` · ${s.error}` : ''),
    }));
  }
  return (
    <InfoModal title="MCP Servers" onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText="No MCP servers configured." />}
    </InfoModal>
  );
}

// --- Agents modal ---

export function AgentsModal({ projectID, app, onClose }) {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    app.GetSlashAgents(projectID)
      .then(setResult)
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  const items = result
    ? (result.agents ?? []).map((a) => ({
        key: a.name,
        name: a.name,
        badge: a.is_builtin ? 'builtin' : 'custom',
        badgeVariant: a.is_builtin ? 'default' : 'ok',
        sub: a.description,
      }))
    : null;
  return (
    <InfoModal title="Agents" onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText="No agents defined." />}
    </InfoModal>
  );
}

// --- Tools modal ---

export function ToolsModal({ projectID, app, onClose }) {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    app.GetSlashTools(projectID)
      .then(setResult)
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  const items = result
    ? (result.tools ?? []).map((t) => ({
        key: t.name,
        name: t.name,
        // Allow is the unremarkable case and stays silent; ask and deny are why
        // a reader looks here.
        badge: t.action && t.action !== 'allow' ? t.action : undefined,
        badgeVariant: t.action === 'deny' ? 'err' : 'default',
        sub: [t.access, t.description].filter(Boolean).join(' · '),
      }))
    : null;
  return (
    <InfoModal title="Tools" onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText="No tools available." />}
    </InfoModal>
  );
}

// --- Worktree modal ---

export function WorktreeModal({ projectID, app, onClose }) {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    app.GetSlashWorktrees(projectID)
      .then(setResult)
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  if (result && !result.available) {
    return (
      <InfoModal title="Worktrees" onClose={onClose}>
        <p className="info-modal__muted">
          This project is not a Git repository, so it has no worktrees.
        </p>
      </InfoModal>
    );
  }

  const items = result
    ? (result.worktrees ?? []).map((w) => ({
        key: w.path,
        name: w.name || w.path,
        badge: w.current ? 'current' : (w.occupied ? 'in use' : undefined),
        badgeVariant: w.current ? 'ok' : 'default',
        sub: [w.branch ? `⎇ ${w.branch}` : '', w.holder ? `held by ${w.holder}` : '']
          .filter(Boolean).join(' · '),
        path: w.path,
      }))
    : null;
  return (
    <InfoModal title="Worktrees" onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText="No worktrees." />}
    </InfoModal>
  );
}

// --- Diff drawer ---

// --- Plugins modal ---

/**
 * PluginsModal is what this project's runtime loaded, and the actions that
 * change it.
 *
 * It reports the resolved inventory rather than the directory: a plugin whose
 * skill the workspace overrides is not contributing that skill however
 * installed it is. Every action rebuilds the runtimes on the Go side, so the
 * list is re-read afterwards rather than patched here.
 */
export function PluginsModal({ projectID, app, onClose }) {
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(null);
  const [installName, setInstallName] = useState('');
  const [plan, setPlan] = useState(null);

  const load = useCallback(() => {
    app.GetPlugins(projectID)
      .then((res) => { setResult(res); setError(null); })
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  useEffect(load, [load]);

  function act(name, run) {
    setBusy(name);
    setError(null);
    run()
      .then(() => { setPlan(null); load(); })
      .catch((err) => setError(err?.message ?? String(err)))
      .finally(() => setBusy(null));
  }

  // Resolving before installing is the point: what a release contributes is
  // worth reading while the decision is still open.
  function preview() {
    const name = installName.trim();
    if (!name) return;
    setBusy(name);
    setError(null);
    app.PlanPluginInstall(name, '', false)
      .then(setPlan)
      .catch((err) => { setPlan(null); setError(err?.message ?? String(err)); })
      .finally(() => setBusy(null));
  }

  const items = result
    ? (result.plugins ?? []).map((p) => ({
        key: p.name,
        name: p.display_name || p.name,
        badge: p.state,
        badgeVariant: p.state === 'active' ? 'ok' : p.state === 'error' ? 'err' : 'default',
        sub: pluginSummary(p),
        path: p.path,
      }))
    : null;

  return (
    <InfoModal title="Plugins" onClose={onClose}>
      {error && <p className="info-modal__error">{error}</p>}
      {result?.allowed_sources?.length ? (
        <p className="info-modal__muted">
          Operator policy allows only: {result.allowed_sources.join(', ')}
        </p>
      ) : null}

      <InfoList items={items} emptyText="No plugins installed." />

      {(result?.plugins ?? []).map((p) => (
        <div key={`actions-${p.name}`} className="info-modal__item-row">
          <span className="info-modal__item-sub">{p.name}</span>
          <button
            type="button"
            className="chat-status-bar__btn"
            disabled={busy === p.name}
            onClick={() => act(p.name, () => app.SetPluginDisabled(p.name, p.state !== 'disabled'))}
          >
            {p.state === 'disabled' ? 'Enable' : 'Disable'}
          </button>
          <button
            type="button"
            className="chat-status-bar__btn"
            disabled={busy === p.name}
            onClick={() => {
              // A checkout may hold work that exists nowhere else, so the Go
              // side refuses one and this asks before overriding that.
              const force = p.source === 'repository'
                && window.confirm(`${p.name} is a Git checkout at ${p.path}.\n\n`
                  + 'Removing it deletes anything uncommitted in it. Continue?');
              if (p.source === 'repository' && !force) return;
              act(p.name, () => app.UninstallPlugin(p.name, force));
            }}
          >
            Remove
          </button>
        </div>
      ))}

      {(result?.notes ?? []).map((note) => (
        <p key={note} className="info-modal__muted">{note}</p>
      ))}

      <div className="info-modal__item-row">
        <input
          type="text"
          value={installName}
          placeholder="plugin name"
          onChange={(e) => { setInstallName(e.target.value); setPlan(null); }}
        />
        <button type="button" className="chat-status-bar__btn" disabled={!installName.trim()} onClick={preview}>
          Find
        </button>
      </div>

      {plan && (
        <div className="info-modal__item">
          <span className="info-modal__item-name">
            {plan.name} {plan.version}
            {plan.already_installed ? ' — already installed' : ''}
          </span>
          <span className="info-modal__item-sub">{planSummary(plan)}</span>
          <span className="info-modal__item-path">{plan.digest}</span>
          {plan.missing_env?.length ? (
            <span className="info-modal__item-sub">
              Reads these, not set here: {plan.missing_env.join(', ')}
            </span>
          ) : null}
          {plan.dirty_source ? (
            <span className="info-modal__item-sub">
              Packed from a working tree with uncommitted changes.
            </span>
          ) : null}
          {!plan.already_installed && (
            <button
              type="button"
              className="chat-status-bar__btn"
              disabled={busy === plan.name}
              onClick={() => act(plan.name, () => app.InstallPlugin(plan.name, plan.version, false))}
            >
              Install
            </button>
          )}
        </div>
      )}
    </InfoModal>
  );
}

/** pluginSummary is one line: where it came from, and what loaded. */
export function pluginSummary(plugin) {
  const parts = [plugin.source];
  if (plugin.version) parts.push(plugin.version);
  else if (plugin.commit) parts.push(plugin.commit.slice(0, 12) + (plugin.dirty ? ' (dirty)' : ''));

  const counts = [
    [plugin.skills, 'skill'],
    [plugin.subagents, 'subagent'],
    [plugin.mcp, 'MCP server'],
    [plugin.hooks, 'hook'],
  ];
  for (const [list, noun] of counts) {
    if (list?.length) parts.push(`${list.length} ${noun}${list.length === 1 ? '' : 's'}`);
  }
  // A plugin that loaded half of what it ships must not read as fully active.
  if (plugin.shadowed?.length) parts.push(`${plugin.shadowed.length} overridden`);
  return parts.join(' · ');
}

/** planSummary says what installing would add. */
export function planSummary(plan) {
  const counts = [
    [plan.skills, 'skill'],
    [plan.subagents, 'subagent'],
    [plan.mcp, 'MCP server'],
    [plan.hooks, 'hook'],
  ];
  const parts = [];
  for (const [list, noun] of counts) {
    if (list?.length) parts.push(`${list.length} ${noun}${list.length === 1 ? '' : 's'}`);
  }
  if (parts.length === 0) return 'contributes nothing this build recognises';
  return parts.join(', ');
}
