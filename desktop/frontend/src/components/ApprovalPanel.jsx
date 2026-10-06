import { useEffect, useState } from 'react';
import { useT } from '../i18n';

// Session grants are what keep a per-write prompt from being something users
// turn off. Keep the outcomes and keys identical to the TUI panel.
const APPROVAL_CHOICES = [
  { decision: 'once',    label: 'chat.approval.once',    variant: 'allow' },
  { decision: 'session', label: 'chat.approval.session', variant: 'allow' },
  { decision: 'deny',    label: 'chat.approval.deny',    variant: 'deny'  },
];

// keys is false for a panel outside the focused pane: shortcuts listen on the
// window, and two visible panels would otherwise both answer one key press.
export function ApprovalPanel({ request, onRespond, keys = true }) {
  const t = useT();
  const [selected, setSelected] = useState(0);

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

  const argEntries = Object.entries(request.args ?? {});

  return (
    <div className="approval-panel">
      <div className="approval-panel__header">
        <span className="approval-panel__title">{t('chat.approval.title')}</span>
        <span className="approval-panel__tool">{request.tool_name}</span>
      </div>

      {argEntries.length > 0 && (
        <table className="approval-panel__args">
          <tbody>
            {argEntries.map(([k, v]) => {
              const val = String(v);
              const display = val.length > 80 ? val.slice(0, 77) + '…' : val;
              return (
                <tr key={k}>
                  <td className="approval-panel__arg-key">{k}</td>
                  <td className="approval-panel__arg-val">{display}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}

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
            onMouseEnter={() => setSelected(i)}
          >
            {t(choice.label)}
          </button>
        ))}
        <span className="approval-panel__hint">{t('chat.approval.hint')}</span>
      </div>
    </div>
  );
}

// --- ProjectItem ---
