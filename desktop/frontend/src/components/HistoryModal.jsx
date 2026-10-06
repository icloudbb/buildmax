import { useCallback, useEffect, useState } from 'react';
import { InfoModal } from './Modals';
import { useStableT, useT } from '../i18n';

/**
 * HistoryModal is the picker behind rewind and fork.
 *
 * Both ask the same question — which message in this conversation — and differ
 * only in what they do with the answer, so one modal serves both and a tab
 * chooses the reading. The lists are not the same list, and the Go side decides
 * which messages each operation may be pointed at: which of them can be handed
 * back or branched from is a rule about the journal, not a presentation choice.
 * It reports the affected span once per point; everything below turns that one
 * number into two different sentences. Those helpers take the caller's
 * translator `t`.
 */

export const REWIND = 'rewind';
export const FORK = 'fork';

/** visiblePoints is the list for one tab, as the Go side computed it. */
export function visiblePoints(points, mode) {
  return (mode === FORK ? points?.fork : points?.rewind) ?? [];
}

export function toolNames(point, t) {
  return (point.tools ?? [])
    .map((tool) => (tool.interrupted ? t('history.interrupted', { name: tool.name }) : tool.name))
    .join(t('chat.list.separator'));
}

/** pointLabel is who spoke. A background event arrives as a user message but
 *  the user did not say it, so the Go side marks it and we do not call it
 *  theirs. */
export function pointLabel(point, t) {
  if (point.role === 'assistant') return t('history.who.agent');
  if (point.role === 'event') return t('history.who.event');
  return t('history.who.you');
}

/**
 * consequenceText is the one-line answer to "what does choosing this do".
 *
 * The same span means opposite things. A rewind removes the chosen prompt and
 * everything after it, hands the prompt back, and leaves the tools' effects
 * behind; a fork removes nothing — the original keeps all of it — but the copy
 * starts without knowing that work happened. Saying "removes" about a fork
 * would be false.
 */
export function consequenceText(point, mode, t) {
  if (!point) return '';
  const tools = toolNames(point, t);
  if (mode === FORK) {
    if (!tools) return t('history.forkCopies');
    return t('history.forkUnaware', { tools });
  }
  const removes = t('history.rewindRemoves', { count: point.messages });
  if (!tools) return t('history.rewindNothingRan', { removes });
  return t('history.rewindLeaves', { removes, tools });
}

/**
 * moveReport is what the user is told afterwards.
 *
 * Rewind names the tools whose effects are still in place, because the
 * conversation no longer mentions them and the workspace still contains them.
 * Fork gives the other half of the same fact: the original lost nothing, and
 * the new session's history does not mention work that nonetheless happened.
 */
export function moveReport(result, mode, restored, t) {
  const tools = toolNames(result ?? {}, t);
  if (mode === FORK) {
    if (!tools) return t('history.report.forkClean');
    return t('history.report.forkTools', { tools });
  }
  const left = tools
    ? t('history.report.rewindTools', { tools })
    : t('history.report.rewindClean');
  return `${left}${promptNote(result ?? {}, restored, t)}`;
}

/**
 * promptNote accounts for the prompt, because the user is looking at a composer
 * and whether what is in it is theirs or the message just taken back is not
 * something to leave them guessing. Only the text comes back, so images the
 * message carried have to be named as not having.
 */
function promptNote(result, restored, t) {
  if (!result.prompt) return '';
  if (!restored) return t('history.report.draftKept');
  const images = result.attachments ?? 0;
  if (!images) return t('history.report.promptBack');
  return t('history.report.imagesLost', { count: images });
}

export function HistoryModal({ projectID, sessionID, app, draft, onRewound, onForked, onClose }) {
  const t = useT();
  const stableT = useStableT();
  const [mode, setMode] = useState(REWIND);
  const [points, setPoints] = useState(null);
  const [selected, setSelected] = useState(null);
  const [error, setError] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    app.GetHistoryPoints(sessionID)
      .then((res) => { setPoints(res ?? {}); setError(null); })
      .catch((err) => setError(err?.message ?? String(err)));
  }, [app, sessionID]);

  useEffect(load, [load]);

  const rows = visiblePoints(points, mode);
  // Switching tabs can hide the selected row, and acting on a point the list no
  // longer shows is how the head gets rewound to itself.
  const current = rows.find((p) => p.item_id === selected) ?? rows[0];

  async function act() {
    if (!current || busy) return;
    setBusy(true);
    setError(null);
    try {
      if (mode === FORK) {
        const res = await app.ForkSession(projectID, sessionID, current.item_id);
        onForked(res.session_id, moveReport(res, FORK, false, stableT));
      } else {
        const res = await app.RewindSession(projectID, sessionID, current.item_id);
        // A draft the user has already typed wins: rewinding is deliberate but
        // the draft is newer, and no branch holds it — the rewound prompt is at
        // least still in the journal.
        const restore = Boolean(res.prompt) && !draft?.trim();
        onRewound(moveReport(res, REWIND, restore, stableT), restore ? res.prompt : '');
      }
      onClose();
    } catch (err) {
      setError(err?.message ?? String(err));
      // The list is stale after a failure that changed nothing, and a busy
      // session is the failure most likely to be over by the time it is read.
      load();
    } finally {
      setBusy(false);
    }
  }

  return (
    <InfoModal title={t('history.title')} onClose={onClose}>
      <div className="history-modal__tabs" role="tablist" aria-label={t('history.tabs')}>
        {[
          [REWIND, t('history.rewind'), t('history.rewindHint')],
          [FORK, t('history.fork'), t('history.forkHint')],
        ].map(([value, label, hint]) => (
          <button
            key={value}
            type="button"
            role="tab"
            aria-selected={mode === value}
            className={`history-modal__tab ${mode === value ? 'history-modal__tab--active' : ''}`}
            onClick={() => setMode(value)}
            title={hint}
          >
            {label}
          </button>
        ))}
      </div>

      {error && <p className="info-modal__error">{error}</p>}

      {!points && <p className="info-modal__muted">{t('shell.loading')}</p>}
      {points && rows.length === 0 && (
        <p className="info-modal__muted">
          {mode === FORK ? t('history.nothingToFork') : t('history.noPrompt')}
        </p>
      )}

      {rows.length > 0 && (
        <ul className="info-modal__list" role="listbox" aria-label={t('history.messages')}>
          {rows.map((p) => (
            <li key={p.item_id}>
              <button
                type="button"
                role="option"
                aria-selected={p.item_id === current?.item_id}
                className={`history-modal__point ${p.item_id === current?.item_id ? 'history-modal__point--active' : ''}`}
                onClick={() => setSelected(p.item_id)}
                onDoubleClick={act}
              >
                <span className="history-modal__who">{pointLabel(p, t)}</span>
                <span className="history-modal__text">{p.content || t('history.noText')}</span>
              </button>
            </li>
          ))}
        </ul>
      )}

      {current && (
        <p className="history-modal__consequence">{consequenceText(current, mode, t)}</p>
      )}

      <div className="modal-footer">
        <button type="button" className="modal-btn modal-btn--cancel" onClick={onClose}>{t('shell.cancel')}</button>
        <button
          type="button"
          className="modal-btn modal-btn--primary"
          onClick={act}
          disabled={!current || busy}
        >
          {mode === FORK ? t('history.forkHere') : t('history.takeBack')}
        </button>
      </div>
    </InfoModal>
  );
}
