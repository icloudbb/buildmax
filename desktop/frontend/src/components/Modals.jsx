import { useCallback, useEffect, useId, useState } from 'react';
import { BaseModal, Button, Field, Input } from '@buildmax/gui';
import { folderBaseName } from '../lib/format';
import { translate, useT } from '../i18n';

const english = (key, vars) => translate('en', key, vars);

export function CreateProjectModal({ app, onCreate, onClose }) {
  const t = useT();
  const titleId = useId();
  const [name, setName] = useState('');
  const [folderPath, setFolderPath] = useState('');
  const [creating, setCreating] = useState(false);
  const [browseError, setBrowseError] = useState(false);

  async function handleBrowse() {
    setBrowseError(false);
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
      setBrowseError(true);
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
    <BaseModal open title={t('shell.newProject')} titleId={titleId} onClose={onClose}>
      <div className="modal__body">
        <Field id="proj-name" label={t('home.create.name')}>
          <Input
            placeholder={t('home.create.namePlaceholder')}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => { if (e.key === 'Enter') folderPath ? handleCreate() : handleBrowse(); }}
            data-autofocus
          />
        </Field>
        <Field group label={t('home.create.folder')} error={browseError ? t('home.create.browseFailed') : undefined}>
          {/* A folder picker, not an action: it shows the chosen path in place. */}
          <button type="button" className="folder-picker" onClick={handleBrowse}>
            {folderPath
              ? <span className="folder-picker__path" title={folderPath}>{folderPath}</span>
              : <span className="folder-picker__placeholder">{t('home.create.chooseFolder')}</span>}
          </button>
        </Field>
        <div className="modal__actions">
          <Button onClick={onClose}>{t('shell.cancel')}</Button>
          <Button
            variant="primary"
            onClick={handleCreate}
            busy={creating}
            disabled={!name.trim() || !folderPath}
          >
            {creating ? t('home.create.creating') : t('home.create.submit')}
          </Button>
        </div>
      </div>
    </BaseModal>
  );
}

// --- Shared info modal ---

// InfoModal is gui's BaseModal in its large, scrolling form, for the lists and
// panels the slash commands open. Callers mount it to open it.
export function InfoModal({ title, onClose, children, className }) {
  const titleId = useId();
  return (
    <BaseModal
      open
      title={title}
      titleId={titleId}
      onClose={onClose}
      className={className ? `modal--large ${className}` : 'modal--large'}
    >
      <div className="modal__body">{children}</div>
    </BaseModal>
  );
}

// ConfirmModal is the one destructive-confirmation dialog for the desktop. It
// replaces window.confirm, which the webview can silently drop (returning
// undefined), and reads as danger: a red title and the danger button.
// It owns its own busy state, awaiting onConfirm so the button shows progress
// and cannot be double-clicked; the caller closes the dialog on success (and
// surfaces any error). `message` may be a string or arbitrary nodes.
export function ConfirmModal({ title, message, confirmLabel, cancelLabel, onConfirm, onCancel }) {
  const t = useT();
  const titleId = useId();
  const [busy, setBusy] = useState(false);
  const confirm = async () => {
    setBusy(true);
    try {
      await onConfirm();
    } finally {
      setBusy(false);
    }
  };
  return (
    <BaseModal
      open
      title={title}
      titleId={titleId}
      onClose={() => { if (!busy) onCancel(); }}
      className="confirm-modal"
    >
      <div className="modal__body">
        {typeof message === 'string'
          ? <p className="confirm-modal__message">{message}</p>
          : message}
        <div className="modal__actions">
          <Button onClick={onCancel} disabled={busy}>
            {cancelLabel ?? t('shell.cancel')}
          </Button>
          <Button variant="danger" onClick={confirm} busy={busy}>
            {busy ? t('shell.working') : (confirmLabel ?? t('shell.delete'))}
          </Button>
        </div>
      </div>
    </BaseModal>
  );
}

export function InfoList({ items, emptyText }) {
  const t = useT();
  if (!items) return <p className="info-modal__muted">{t('shell.loading')}</p>;
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
  const t = useT();
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
        <InfoModal title={t('chat.mcp.title')} onClose={onClose}>
          <p className="info-modal__error">{result.load_error}</p>
        </InfoModal>
      );
    }
    items = (result.servers ?? []).map((s) => ({
      key: s.id,
      name: s.id,
      badge: s.ok ? t('chat.mcp.tools', { count: s.tool_count }) : t('chat.mcp.error'),
      badgeVariant: s.ok ? 'ok' : 'err',
      sub: s.type + ((!s.ok && s.error) ? ` · ${s.error}` : ''),
    }));
  }
  return (
    <InfoModal title={t('chat.mcp.title')} onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText={t('chat.mcp.empty')} />}
    </InfoModal>
  );
}

// --- Agents modal ---

export function AgentsModal({ projectID, app, onClose }) {
  const t = useT();
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
        badge: a.is_builtin ? t('chat.agents.builtin') : t('chat.agents.custom'),
        badgeVariant: a.is_builtin ? 'default' : 'ok',
        sub: a.description,
      }))
    : null;
  return (
    <InfoModal title={t('chat.agents.title')} onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText={t('chat.agents.empty')} />}
    </InfoModal>
  );
}

// --- Tools modal ---

export function ToolsModal({ projectID, app, onClose }) {
  const t = useT();
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    app.GetSlashTools(projectID)
      .then(setResult)
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  const items = result
    ? (result.tools ?? []).map((tool) => ({
        key: tool.name,
        name: tool.name,
        // Allow is the unremarkable case and stays silent; ask and deny are why
        // a reader looks here.
        badge: tool.action && tool.action !== 'allow' ? toolActionLabel(tool.action, t) : undefined,
        badgeVariant: tool.action === 'deny' ? 'err' : 'default',
        sub: [tool.access, tool.description].filter(Boolean).join(' · '),
      }))
    : null;
  return (
    <InfoModal title={t('chat.tools.title')} onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText={t('chat.tools.empty')} />}
    </InfoModal>
  );
}

// --- Worktree modal ---

export function WorktreeModal({ projectID, app, onClose }) {
  const t = useT();
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  useEffect(() => {
    app.GetSlashWorktrees(projectID)
      .then(setResult)
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  if (result && !result.available) {
    return (
      <InfoModal title={t('chat.worktrees.title')} onClose={onClose}>
        <p className="info-modal__muted">{t('chat.worktrees.notGit')}</p>
      </InfoModal>
    );
  }

  const items = result
    ? (result.worktrees ?? []).map((w) => ({
        key: w.path,
        name: w.name || w.path,
        badge: w.current ? t('chat.worktrees.current') : (w.occupied ? t('chat.worktrees.inUse') : undefined),
        badgeVariant: w.current ? 'ok' : 'default',
        sub: [w.branch ? `⎇ ${w.branch}` : '', w.holder ? t('chat.worktrees.heldBy', { holder: w.holder }) : '']
          .filter(Boolean).join(' · '),
        path: w.path,
      }))
    : null;
  return (
    <InfoModal title={t('chat.worktrees.title')} onClose={onClose}>
      {error
        ? <p className="info-modal__error">{error}</p>
        : <InfoList items={items} emptyText={t('chat.worktrees.empty')} />}
    </InfoModal>
  );
}

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
  const t = useT();
  const [result, setResult] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(null);
  const [installName, setInstallName] = useState('');
  const [plan, setPlan] = useState(null);
  // A repository-checkout plugin awaiting remove confirmation (deleting it can
  // destroy uncommitted work), shown in a ConfirmModal instead of window.confirm.
  const [pendingRemove, setPendingRemove] = useState(null);

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
        badge: PLUGIN_STATES.has(p.state) ? t(`chat.plugins.state.${p.state}`) : p.state,
        badgeVariant: p.state === 'active' ? 'ok' : p.state === 'error' ? 'err' : 'default',
        sub: pluginSummary(p, t),
        path: p.path,
      }))
    : null;

  return (
    <>
    <InfoModal title={t('chat.plugins.title')} onClose={onClose}>
      {error && <p className="info-modal__error">{error}</p>}
      {result?.allowed_sources?.length ? (
        <p className="info-modal__muted">
          {t('chat.plugins.allowedSources', { sources: result.allowed_sources.join(t('chat.list.separator')) })}
        </p>
      ) : null}

      <InfoList items={items} emptyText={t('chat.plugins.empty')} />

      {(result?.plugins ?? []).map((p) => (
        <div key={`actions-${p.name}`} className="info-modal__item-row">
          <span className="info-modal__item-sub">{p.name}</span>
          <Button
            size="compact"
            disabled={busy === p.name}
            onClick={() => act(p.name, () => app.SetPluginDisabled(p.name, p.state !== 'disabled'))}
          >
            {p.state === 'disabled' ? t('chat.plugins.enable') : t('chat.plugins.disable')}
          </Button>
          <Button
            size="compact"
            disabled={busy === p.name}
            onClick={() => {
              // A checkout may hold work that exists nowhere else, so the Go
              // side refuses one and this asks before overriding that.
              if (p.source === 'repository') { setPendingRemove(p); return; }
              act(p.name, () => app.UninstallPlugin(p.name, false));
            }}
          >
            {t('shell.remove')}
          </Button>
        </div>
      ))}

      {(result?.notes ?? []).map((note) => (
        <p key={note} className="info-modal__muted">{note}</p>
      ))}

      <div className="info-modal__item-row">
        <input
          type="text"
          value={installName}
          placeholder={t('chat.plugins.namePlaceholder')}
          onChange={(e) => { setInstallName(e.target.value); setPlan(null); }}
        />
        <Button size="compact" disabled={!installName.trim()} onClick={preview}>
          {t('chat.plugins.find')}
        </Button>
      </div>

      {plan && (
        <div className="info-modal__item">
          <span className="info-modal__item-name">
            {plan.name} {plan.version}
            {plan.already_installed ? t('chat.plugins.alreadyInstalled') : ''}
          </span>
          <span className="info-modal__item-sub">{planSummary(plan, t)}</span>
          <span className="info-modal__item-path">{plan.digest}</span>
          {plan.missing_env?.length ? (
            <span className="info-modal__item-sub">
              {t('chat.plugins.missingEnv', { names: plan.missing_env.join(t('chat.list.separator')) })}
            </span>
          ) : null}
          {plan.dirty_source ? (
            <span className="info-modal__item-sub">{t('chat.plugins.dirtySource')}</span>
          ) : null}
          {!plan.already_installed && (
            <Button
              size="compact"
              variant="primary"
              disabled={busy === plan.name}
              onClick={() => act(plan.name, () => app.InstallPlugin(plan.name, plan.version, false))}
            >
              {t('chat.plugins.install')}
            </Button>
          )}
        </div>
      )}
    </InfoModal>
    {pendingRemove && (
      <ConfirmModal
        title={t('chat.plugins.removeTitle')}
        confirmLabel={t('shell.remove')}
        message={t('chat.plugins.removeMessage', { name: pendingRemove.name, path: pendingRemove.path })}
        onCancel={() => setPendingRemove(null)}
        onConfirm={async () => {
          const p = pendingRemove;
          setPendingRemove(null);
          act(p.name, () => app.UninstallPlugin(p.name, true));
        }}
      />
    )}
    </>
  );
}

// The plugin states with a label; any other is shown as the runtime sent it.
const PLUGIN_STATES = new Set(['active', 'disabled', 'error']);

function toolActionLabel(action, t) {
  if (action === 'ask') return t('chat.tools.ask');
  if (action === 'deny') return t('chat.tools.deny');
  return action;
}

// What a plugin or a release contributes, counted per kind.
function contributionCounts(source, t) {
  const counts = [
    [source.skills, 'chat.plugins.skills'],
    [source.subagents, 'chat.plugins.subagents'],
    [source.mcp, 'chat.plugins.mcp'],
    [source.hooks, 'chat.plugins.hooks'],
  ];
  return counts.filter(([list]) => list?.length).map(([list, key]) => t(key, { count: list.length }));
}

/** pluginSummary is one line: where it came from, and what loaded. */
export function pluginSummary(plugin, t = english) {
  const parts = [plugin.source];
  if (plugin.version) parts.push(plugin.version);
  else if (plugin.commit) parts.push(plugin.commit.slice(0, 12) + (plugin.dirty ? t('chat.plugins.dirty') : ''));
  parts.push(...contributionCounts(plugin, t));
  // A plugin that loaded half of what it ships must not read as fully active.
  if (plugin.shadowed?.length) parts.push(t('chat.plugins.overridden', { count: plugin.shadowed.length }));
  return parts.join(' · ');
}

/** planSummary says what installing would add. */
export function planSummary(plan, t = english) {
  const parts = contributionCounts(plan, t);
  if (parts.length === 0) return t('chat.plugins.contributesNothing');
  return parts.join(t('chat.list.separator'));
}
