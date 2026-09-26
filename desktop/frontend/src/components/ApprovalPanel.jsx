import { useEffect, useState } from 'react';

// Session grants are what keep a per-write prompt from being something users
// turn off. Keep the outcomes and keys identical to the TUI panel.
const APPROVAL_CHOICES = [
  { decision: 'once',    label: 'Allow once(y)',    variant: 'allow' },
  { decision: 'session', label: 'Allow session(a)', variant: 'allow' },
  { decision: 'deny',    label: 'Deny(n)',          variant: 'deny'  },
];

// keys is false for a panel outside the focused pane: shortcuts listen on the
// window, and two visible panels would otherwise both answer one key press.
export function ApprovalPanel({ request, onRespond, keys = true }) {
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
        <span className="approval-panel__title">Tool Approval</span>
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

      <div className="approval-panel__footer">
        {APPROVAL_CHOICES.map((choice, i) => (
          <button
            key={choice.decision}
            type="button"
            className={`approval-panel__btn ${selected === i ? `approval-panel__btn--${choice.variant}` : 'approval-panel__btn--muted'}`}
            onClick={() => onRespond(choice.decision)}
            onMouseEnter={() => setSelected(i)}
          >
            {choice.label}
          </button>
        ))}
        <span className="approval-panel__hint">← → select · Enter confirm</span>
      </div>
    </div>
  );
}

// --- ProjectItem ---
