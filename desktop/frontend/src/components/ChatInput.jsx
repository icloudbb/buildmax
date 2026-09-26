import { useEffect, useRef, useState } from 'react';
import { ChatComposer } from '@buildmax/gui';
import { formatTokenCount } from '../lib/format';
import { ApprovalPanel } from './ApprovalPanel';
import { JobsDrawer } from './JobsDrawer';
import { HistoryModal } from './HistoryModal';
import { AgentsModal, MCPModal, PluginsModal, ToolsModal, WorktreeModal } from './Modals';
import { EventsOn } from '../lib/wailsRuntime';

// CommandPalette is the "/" overlay: the slash commands the surface offers,
// then the skills, filtered by what follows the slash. It is the Desktop
// counterpart to the TUI's completion popup — selecting a command dispatches
// it, selecting a skill drops its "/name" into the composer to send.
export function CommandPalette({ items, selected, onSelect, onHighlight }) {
  if (!items.length) {
    return (
      <div className="slash-popup">
        <div className="slash-popup__title">Commands</div>
        <p className="slash-popup__empty">No match.</p>
      </div>
    );
  }
  return (
    <div className="slash-popup" role="listbox" aria-label="Commands">
      <div className="slash-popup__title">Commands &amp; skills</div>
      {items.map((item, i) => (
        <button
          key={item.key}
          type="button"
          role="option"
          aria-selected={i === selected}
          disabled={item.disabled}
          className={`slash-popup__item ${i === selected ? 'slash-popup__item--active' : ''} ${item.disabled ? 'slash-popup__item--disabled' : ''}`}
          onMouseEnter={() => onHighlight(i)}
          onClick={() => onSelect(item)}
          title={item.disabled ? 'Send a message first' : item.description}
        >
          <span className="slash-popup__cmd">
            /{item.name}
            {item.kind === 'skill' && <span className="slash-popup__tag"> skill</span>}
          </span>
          {item.description && <span className="slash-popup__desc">{item.description}</span>}
        </button>
      ))}
    </div>
  );
}

// ContextDonut is the status bar's context-window gauge. It replaces the old
// "ctx: N%" text with a ring whose filled arc is the share of the window in
// use, and turns amber then red as the window fills so a run nearing compaction
// reads at a glance. Clicking it opens the exact counts a ring can only
// approximate.
export function ContextDonut({ status }) {
  const [open, setOpen] = useState(false);
  const ref = useRef(null);

  useEffect(() => {
    if (!open) return undefined;
    function onOutside(e) {
      if (ref.current && !ref.current.contains(e.target)) setOpen(false);
    }
    function onKey(e) { if (e.key === 'Escape') setOpen(false); }
    document.addEventListener('mousedown', onOutside);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onOutside);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const used = Number(status?.context_tokens) || 0;
  const limit = Number(status?.context_window) || 0;
  const known = limit > 0;
  const share = known ? Math.min(used / limit, 1) : 0;
  const pct = Math.round(share * 100);
  const free = Math.max(limit - used, 0);

  // Ring geometry: the fill arc starts at 12 o'clock (the -90° rotation) and
  // grows clockwise with the share.
  const radius = 7;
  const circumference = 2 * Math.PI * radius;
  const level = pct >= 90 ? 'danger' : pct >= 75 ? 'warn' : 'ok';
  const label = known ? `Context: ${pct}% used` : 'Context usage unknown';

  return (
    <div className="ctx-donut" ref={ref}>
      <button
        type="button"
        className="ctx-donut__btn"
        onClick={() => setOpen((v) => !v)}
        aria-label={label}
        aria-expanded={open}
        title={label}
      >
        <svg viewBox="0 0 20 20" width="15" height="15" aria-hidden focusable="false">
          <circle className="ctx-donut__track" cx="10" cy="10" r={radius} fill="none" strokeWidth="4" />
          {known && (
            <circle
              className={`ctx-donut__fill ctx-donut__fill--${level}`}
              cx="10"
              cy="10"
              r={radius}
              fill="none"
              strokeWidth="4"
              strokeDasharray={`${share * circumference} ${circumference}`}
              strokeLinecap={share > 0 ? 'round' : 'butt'}
              transform="rotate(-90 10 10)"
            />
          )}
        </svg>
      </button>

      {open && (
        <div className="ctx-donut__popover" role="dialog" aria-label="Context window">
          {known ? (
            <>
              <div className="ctx-donut__popover-pct">{pct}%</div>
              <dl className="ctx-donut__popover-list">
                <div><dt>Used</dt><dd>{formatTokenCount(used)}</dd></div>
                <div><dt>Free</dt><dd>{formatTokenCount(free)}</dd></div>
                <div><dt>Window</dt><dd>{formatTokenCount(limit)}</dd></div>
              </dl>
            </>
          ) : (
            <p className="ctx-donut__popover-empty">No context reading yet.</p>
          )}
        </div>
      )}
    </div>
  );
}

// --- ChatInput ---

export function ChatInput({ draft = null, onDraftConsumed, onSend, onCancel, loading, error, onDismissError, currentProject, app, approvalRequest, onRespond, approvalKeys = true, toolActivity, runStatus, sessionId, onRunStatusContext, onRewound, onForked, onCompacted, onCommandError, suggestion, onAcceptSuggestion, onShowInfo, onShowChanges, infoOpen, onToggleInfo }) {
  const [prompt, setPrompt] = useState('');

  // A draft handed in (an Issue's "Start chat") fills the composer once and is
  // released, so it is never sent without the person pressing Send.
  useEffect(() => {
    if (!draft) return;
    setPrompt(draft.text);
    onDraftConsumed?.();
  }, [draft, onDraftConsumed]);

  // Palette state.
  const [commands, setCommands] = useState([]);
  const [skills, setSkills] = useState([]);
  const [selected, setSelected] = useState(0);

  // Status bar state (loaded per project)
  const [models, setModels] = useState([]);
  // Where this session's prompts go. It is the app's mode rather than a property
  // of one model, so the picker says it once above the list.
  const [modelMode, setModelMode] = useState('');
  const [currentModel, setCurrentModel] = useState('');
  const [gitBranch, setGitBranch] = useState('');
  const [showModelDropdown, setShowModelDropdown] = useState(false);
  const modelDropdownRef = useRef(null);

  // Lazy-loaded panel state, one per command that opens a panel.
  const [showMCP, setShowMCP] = useState(false);
  const [showAgents, setShowAgents] = useState(false);
  const [showPlugins, setShowPlugins] = useState(false);
  const [showJobs, setShowJobs] = useState(false);
  const [showTools, setShowTools] = useState(false);
  const [showWorktree, setShowWorktree] = useState(false);
  const [showHistory, setShowHistory] = useState(false);

  // Count of running background jobs for the status-bar badge. The Go side
  // owns job state; this refreshes from it on every lifecycle event.
  const [runningJobs, setRunningJobs] = useState(0);
  useEffect(() => {
    if (!currentProject || !app?.ListJobs) return undefined;
    let cancelled = false;
    const refresh = () => {
      app.ListJobs(currentProject.id)
        .then((list) => { if (!cancelled) setRunningJobs((list ?? []).filter((j) => j.running).length); })
        .catch(() => {});
    };
    refresh();
    const unsub = EventsOn('desktop/job-update', (payload) => {
      if (payload?.project_id === currentProject.id) refresh();
    });
    return () => { cancelled = true; unsub(); };
  }, [currentProject, app]);

  // Load project-level data when project changes.
  useEffect(() => {
    if (!currentProject || !app) return;
    setModels([]);
    setCurrentModel('');
    setSkills([]);
    Promise.allSettled([
      // The model list and its mode are project-scoped, so they load here with
      // an empty session; the active model is per conversation and tracked in
      // the effect below, which re-resolves it whenever the session changes.
      app.GetSlashModels(currentProject.id, ''),
      app.GetSlashSkills(currentProject.id),
      // Guarded: a call to a binding the running app has not regenerated yet
      // would throw synchronously here and take the model and skill loads down
      // with it. The palette simply has no commands until the app is rebuilt.
      app.GetSlashCommands ? app.GetSlashCommands() : Promise.resolve([]),
    ]).then(([modelsRes, skillsRes, cmdRes]) => {
      if (modelsRes.status === 'fulfilled') {
        setModels(modelsRes.value.models ?? []);
        setModelMode(
          modelsRes.value.managed
            ? `Prompts go to ${modelsRes.value.server_url}`
            : 'Prompts go from this machine to each provider',
        );
      }
      if (skillsRes.status === 'fulfilled') setSkills(skillsRes.value.skills ?? []);
      if (cmdRes.status === 'fulfilled') setCommands(cmdRes.value ?? []);
    });
  }, [currentProject, app]);

  // The active model and git branch are per session, not per project: a
  // session may run in a worktree distinct from the project's default
  // workspace, so both re-resolve when the session changes, not only when the
  // project does. An empty session (a chat not yet started) reports the
  // project's own default (model) or its default workspace (branch).
  useEffect(() => {
    if (!currentProject || !app) return undefined;
    let cancelled = false;
    app.GetSlashModels(currentProject.id, sessionId || '')
      .then((res) => { if (!cancelled) setCurrentModel(res.current ?? ''); })
      .catch(() => {});
    app.GetGitBranch(currentProject.id, sessionId || '')
      .then((branch) => { if (!cancelled) setGitBranch(branch ?? ''); })
      .catch(() => { if (!cancelled) setGitBranch(''); });
    return () => { cancelled = true; };
  }, [currentProject, app, sessionId]);

  // Close model dropdown on outside click.
  useEffect(() => {
    if (!showModelDropdown) return;
    function onOutside(e) {
      if (modelDropdownRef.current && !modelDropdownRef.current.contains(e.target)) {
        setShowModelDropdown(false);
      }
    }
    document.addEventListener('mousedown', onOutside);
    return () => document.removeEventListener('mousedown', onOutside);
  }, [showModelDropdown]);

  // Palette: trigger on a leading "/"; the text after it filters. A space means
  // the command name is finished and arguments have begun, so the palette hides.
  const paletteFilter = (() => {
    const first = prompt.split('\n')[0].trimStart();
    if (!first.startsWith('/')) return null;
    const after = first.slice(1);
    if (after.includes(' ')) return null;
    return after;
  })();
  const showPalette = paletteFilter !== null;

  // The palette's items: matching commands first, then matching skills. A
  // command that acts on a saved session is disabled until there is one.
  const paletteItems = showPalette
    ? [
        ...commands
          .filter((c) => !paletteFilter || c.name.toLowerCase().includes(paletteFilter.toLowerCase()))
          .map((c) => ({
            key: `cmd:${c.name}`,
            kind: 'command',
            name: c.name,
            description: c.description,
            disabled: c.requires_session && !sessionId,
          })),
        ...skills
          .filter((s) => !paletteFilter || s.name.toLowerCase().includes(paletteFilter.toLowerCase()))
          .map((s) => ({
            key: `skill:${s.name}`,
            kind: 'skill',
            name: s.name,
            description: s.description,
            disabled: false,
          })),
      ]
    : [];

  const isCommand = (name) => commands.some((c) => c.name === name);

  // dispatchCommand runs a command by name: it opens the panel or performs the
  // action, mirroring the TUI's dispatchSlashCommand. The input is cleared so
  // the typed command does not linger.
  function dispatchCommand(name) {
    setPrompt('');
    setSelected(0);
    switch (name) {
      case 'model': setShowModelDropdown(true); break;
      case 'diff': onShowChanges?.(); break;
      case 'mcp': setShowMCP(true); break;
      case 'agents': setShowAgents(true); break;
      case 'plugins': setShowPlugins(true); break;
      case 'tasks': setShowJobs(true); break;
      case 'tools': setShowTools(true); break;
      case 'worktree': setShowWorktree(true); break;
      case 'info': onShowInfo?.(); break;
      case 'rewind':
      case 'fork':
        if (sessionId) setShowHistory(true);
        break;
      case 'compact': handleCompact(); break;
      case 'skills': /* skills are listed inline in the palette */ break;
      default: break;
    }
  }

  async function handleCompact() {
    if (!sessionId) return;
    try {
      const res = await app.CompactProjectSession(currentProject.id, sessionId);
      onCompacted?.(res);
    } catch (err) {
      onCommandError?.(err?.message ?? String(err));
    }
  }

  function handleSubmit() {
    const value = prompt.trim();
    if (!value) return;
    // A bare command (optionally with arguments) is dispatched, not sent: the
    // first token names it. Anything else — including a "/skillname" the user
    // typed freehand — is sent as a message.
    if (value.startsWith('/')) {
      const firstToken = value.slice(1).split(/\s+/)[0];
      if (isCommand(firstToken)) {
        dispatchCommand(firstToken);
        return;
      }
    }
    // No loading guard: onSend queues the message when a run is in flight.
    onSend(value);
    setPrompt('');
  }

  function handleChange(value) {
    setPrompt(value);
    onDismissError();
    setSelected(0);
  }

  // Accepting puts the suggestion in the box for the user to send or edit; it
  // is never sent on their behalf.
  function handleAcceptSuggestion() {
    setPrompt(suggestion);
    onAcceptSuggestion?.();
  }

  function handleSelectItem(item) {
    if (item.disabled) return;
    if (item.kind === 'command') {
      dispatchCommand(item.name);
      return;
    }
    setPrompt(`/${item.name}`);
    setSelected(0);
  }

  async function handleModelSwitch(modelName) {
    setShowModelDropdown(false);
    try {
      await app.SetProjectModel(currentProject.id, sessionId || '', modelName);
      setCurrentModel(modelName);
      const status = await app.GetRunStatus(currentProject.id, sessionId || '');
      onRunStatusContext?.(status);
    } catch { /* ignore */ }
  }

  function handleKeyDown(e) {
    if (!showPalette || !paletteItems.length) return;
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setSelected((p) => (p - 1 + paletteItems.length) % paletteItems.length);
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      setSelected((p) => (p + 1) % paletteItems.length);
    } else if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      const item = paletteItems[selected];
      if (item) handleSelectItem(item);
    } else if (e.key === 'Escape') {
      setPrompt('');
    }
  }

  const displayModel = currentModel
    ? (currentModel.length > 24 ? currentModel.slice(0, 22) + '…' : currentModel)
    : 'No model';

  return (
    <div className="chat-input-wrap">
      {approvalRequest && (
        <ApprovalPanel key={approvalRequest.approval_id} request={approvalRequest} onRespond={onRespond} keys={approvalKeys} />
      )}

      {toolActivity && (
        <div className="tool-activity-bar">{toolActivity}</div>
      )}

      {showPalette && (
        <CommandPalette
          items={paletteItems}
          selected={selected}
          onSelect={handleSelectItem}
          onHighlight={setSelected}
        />
      )}

      <ChatComposer
        value={prompt}
        onChange={handleChange}
        onSubmit={handleSubmit}
        onCancel={onCancel}
        loading={loading}
        error={error}
        placeholder="Type a message… (/ for commands, Enter to send)"
        queueWhileLoading
        queuePlaceholder="Type a message… (Enter to queue it for the next turn)"
        ariaLabel="Message"
        onKeyDown={handleKeyDown}
        ghost={suggestion}
        onAcceptGhost={handleAcceptSuggestion}
      />

      <div className="chat-status-bar">
        {/* Model dropdown — kept as a persistent control; /model opens it too. */}
        <div className="model-selector" ref={modelDropdownRef}>
          <button
            type="button"
            className="model-selector__btn"
            onClick={() => setShowModelDropdown((v) => !v)}
            title={currentModel || 'Select model'}
          >
            <span className="model-selector__label">{displayModel}</span>
            <span className="model-selector__arrow" aria-hidden>▾</span>
          </button>
          {showModelDropdown && models.length > 0 && (
            <div className="model-selector__dropdown" role="listbox">
              {modelMode && (
                <div className="model-selector__mode">{modelMode}</div>
              )}
              {models.map((m) => {
                // Derive the active mark from currentModel, the state a switch
                // updates. The is_current flags from GetSlashModels are computed
                // once at load and go stale after every SetProjectModel.
                const isCurrent = m.name === currentModel || m.provider_model === currentModel;
                return (
                  <button
                    key={m.name}
                    type="button"
                    role="option"
                    aria-selected={isCurrent}
                    className={`model-selector__option ${isCurrent ? 'model-selector__option--active' : ''}`}
                    onClick={() => handleModelSwitch(m.name)}
                  >
                    <span className="model-selector__option-name">{m.name}</span>
                    {m.provider_model && m.provider_model !== m.name && (
                      <span className="model-selector__option-sub">{m.provider_model}</span>
                    )}
                    {isCurrent && <span className="model-selector__option-check" aria-hidden>✓</span>}
                  </button>
                );
              })}
            </div>
          )}
        </div>

        {/* Git branch */}
        {gitBranch && (
          <span className="chat-status-bar__branch" title={`Branch: ${gitBranch}`}>
            ⎇ {gitBranch}
          </span>
        )}

        <ContextDonut status={runStatus} />

        {onToggleInfo && (
          <button
            type="button"
            className={`chat-status-bar__info ${infoOpen ? 'chat-status-bar__info--active' : ''}`}
            onClick={onToggleInfo}
            aria-pressed={!!infoOpen}
            title="Session info"
            aria-label="Session info"
          >
            <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
              <circle cx="12" cy="12" r="9" />
              <path d="M12 11v5" />
              <circle cx="12" cy="7.75" r="0.6" fill="currentColor" stroke="none" />
            </svg>
          </button>
        )}

        <div className="chat-status-bar__spacer" />

        {/* Everything the status bar used to offer as buttons is now a slash
            command; the hint says how to reach them. */}
        <span className="chat-status-bar__hint">
          {runningJobs > 0 ? `${runningJobs} running · ` : ''}/ for commands
        </span>
      </div>

      {showMCP && (
        <MCPModal projectID={currentProject.id} app={app} onClose={() => setShowMCP(false)} />
      )}
      {showAgents && (
        <AgentsModal projectID={currentProject.id} app={app} onClose={() => setShowAgents(false)} />
      )}
      {showPlugins && (
        <PluginsModal projectID={currentProject.id} app={app} onClose={() => setShowPlugins(false)} />
      )}
      {showTools && (
        <ToolsModal projectID={currentProject.id} app={app} onClose={() => setShowTools(false)} />
      )}
      {showWorktree && (
        <WorktreeModal projectID={currentProject.id} app={app} onClose={() => setShowWorktree(false)} />
      )}
      {showJobs && (
        <JobsDrawer projectID={currentProject.id} app={app} onClose={() => setShowJobs(false)} />
      )}
      {showHistory && sessionId && (
        <HistoryModal
          projectID={currentProject.id}
          sessionID={sessionId}
          app={app}
          draft={prompt}
          onRewound={(report, restored) => {
            // The rewound prompt comes back here to be edited and sent again;
            // the modal decides whether it may, since a draft already in the
            // composer wins.
            if (restored) setPrompt(restored);
            onRewound(report);
          }}
          onForked={onForked}
          onClose={() => setShowHistory(false)}
        />
      )}
    </div>
  );
}
