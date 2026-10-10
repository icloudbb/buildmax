import { useEffect, useMemo, useState } from 'react';
import { useT } from '../i18n';
import { diffLines } from '../lib/linediff';
import { proposedChange } from '../lib/toolChange';
import { LineDiff } from './LineDiff';

// Session grants are what keep a per-write prompt from being something users
// turn off. Keep the outcomes and keys identical to the TUI panel.
const APPROVAL_CHOICES = [
  { decision: 'once',    label: 'chat.approval.once',    variant: 'allow' },
  { decision: 'session', label: 'chat.approval.session', variant: 'allow' },
  { decision: 'deny',    label: 'chat.approval.deny',    variant: 'deny'  },
];

// The choice Enter confirms until the person moves it with the arrow keys:
// the narrowest grant, whatever an earlier prompt was answered with.
const DEFAULT_CHOICE = 0;

const EFFECT_KEY = {
  edit: 'chat.approval.effect.edit',
  create: 'chat.approval.effect.create',
  overwrite: 'chat.approval.effect.overwrite',
  empty: 'chat.approval.effect.empty',
  write: 'chat.approval.effect.write',
  'edit-missing': 'chat.approval.effect.editMissing',
  'edit-ambiguous': 'chat.approval.effect.editAmbiguous',
  'edit-no-file': 'chat.approval.effect.editNoFile',
};

const REASON_KEY = {
  outside_root: 'chat.approval.unavailable.outsideRoot',
  binary: 'chat.approval.unavailable.binary',
  too_large: 'chat.approval.unavailable.tooLarge',
  not_a_file: 'chat.approval.unavailable.notAFile',
  unreadable: 'chat.approval.unavailable.unreadable',
};

// keys is false for a panel outside the focused pane: shortcuts listen on the
// window, and two visible panels would otherwise both answer one key press.
export function ApprovalPanel({ request, onRespond, keys = true }) {
  const t = useT();
  // Only the arrow keys move the selection. Hover used to as well, so a new
  // prompt appearing under a pointer left resting on "Allow session" made
  // Enter grant the session.
  const [selected, setSelected] = useState(DEFAULT_CHOICE);

  useEffect(() => {
    if (!keys) return undefined;
    function onKey(e) {
      switch (e.key) {
        case 'ArrowLeft':  setSelected((i) => Math.max(0, i - 1)); break;
        case 'ArrowRight': setSelected((i) => Math.min(APPROVAL_CHOICES.length - 1, i + 1)); break;
        case 'Enter':      onRespond(APPROVAL_CHOICES[selected].decision); break;
        case 'y': case 'Y': onRespond('once'); break;
        case 'a': case 'A': onRespond('session'); break;
        case 'n': case 'N': case 'Escape': onRespond('deny'); break;
      }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selected, onRespond, keys]);

  const change = useMemo(() => proposedChange(request), [request]);

  return (
    <div className="approval-panel">
      <div className="approval-panel__header">
        <span className="approval-panel__title">{t('chat.approval.title')}</span>
        <span className="approval-panel__tool">{request.tool_name}</span>
      </div>

      {change ? <FileChange change={change} /> : <ToolArgs args={request.args} />}

      {/* What "Allow session" admits when the tool narrows it: one browser
          origin or one MCP server/tool, not every later call. */}
      {request.target && (
        <div className="approval-panel__target">
          {t('chat.approval.scope')} <code>{request.target}</code>
        </div>
      )}

      <div className="approval-panel__footer">
        {APPROVAL_CHOICES.map((choice, i) => (
          <button
            key={choice.decision}
            type="button"
            className={`approval-panel__btn ${selected === i ? `approval-panel__btn--${choice.variant}` : 'approval-panel__btn--muted'}`}
            onClick={() => onRespond(choice.decision)}
          >
            {t(choice.label)}
          </button>
        ))}
        <span className="approval-panel__hint">{t('chat.approval.hint')}</span>
      </div>
    </div>
  );
}

// FileChange states what an Edit or Write does to its file, then shows it as
// a diff against the file as it is now.
function FileChange({ change }) {
  const t = useT();
  const hunks = useMemo(
    () => diffLines(change.before, change.after, { context: change.plain ? Infinity : 3 }),
    [change],
  );
  const failing = change.effect.startsWith('edit-');
  return (
    <div className="approval-panel__change">
      <p className={`approval-panel__effect${failing ? ' approval-panel__effect--fail' : ''}`}>
        {t(EFFECT_KEY[change.effect], { path: change.path })}
        {change.replaceAll && <> {t('chat.approval.replaceAll')}</>}
      </p>
      {change.reason && (
        <p className="approval-panel__note">{t(REASON_KEY[change.reason] ?? REASON_KEY.unreadable, { path: change.path })}</p>
      )}
      {hunks.length > 0 ? (
        <div className="approval-panel__scroll">
          <LineDiff hunks={hunks} numbers={!change.plain} label={t('chat.approval.diffLabel', { path: change.path })} />
        </div>
      ) : (
        <p className="approval-panel__note">{t('chat.approval.noLineChanges')}</p>
      )}
    </div>
  );
}

// ToolArgs lists any other tool's arguments with their text as given:
// newlines and indentation are part of what is being approved.
function ToolArgs({ args }) {
  const entries = Object.entries(args ?? {});
  if (!entries.length) return null;
  return (
    <dl className="approval-panel__args approval-panel__scroll">
      {entries.map(([k, v]) => (
        <div key={k} className="approval-panel__arg">
          <dt className="approval-panel__arg-key">{k}</dt>
          <dd className="approval-panel__arg-val">
            <pre>{typeof v === 'string' ? v : JSON.stringify(v, null, 2)}</pre>
          </dd>
        </div>
      ))}
    </dl>
  );
}
