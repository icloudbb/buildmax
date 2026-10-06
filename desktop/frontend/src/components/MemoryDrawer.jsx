import { useEffect, useState } from 'react';
import { useLocale } from '@buildmax/gui';
import { useT } from '../i18n';
import { intlLocale } from '../lib/format';

function formatWritten(entry, t, locale) {
  if (!entry.updated_at) return '';
  const at = Date.parse(entry.updated_at);
  if (Number.isNaN(at)) return '';
  const written = new Date(at).toLocaleString(intlLocale(locale));
  return entry.verified_at ? t('memory.verified', { written, verified: entry.verified_at }) : written;
}

// MemoryView lists what the project remembers and shows one memory's body. It
// is the memory half of the /info panel, over the same store the CLI and TUI
// read; the panel provides the surrounding tab shell.
//
// Read-only. A memory is a Markdown file the user can edit directly, and the
// directory is shown so they can; editing here needs the refusal path a
// digest-checked write can take, which is its own piece of work.
export function MemoryView({ projectID, app }) {
  const t = useT();
  const { locale } = useLocale();
  const [payload, setPayload] = useState(null);
  const [error, setError] = useState(null);
  const [selectedName, setSelectedName] = useState('');

  useEffect(() => {
    let cancelled = false;
    app.ProjectMemory(projectID)
      .then((p) => {
        if (cancelled) return;
        setPayload(p ?? null);
        setSelectedName((cur) => cur || (p?.memories?.[0]?.name ?? ''));
      })
      .catch((err) => { if (!cancelled) setError(err?.message ?? String(err)); });
    return () => { cancelled = true; };
  }, [projectID, app]);

  const memories = payload?.memories ?? [];
  const skipped = payload?.skipped ?? [];
  const selected = memories.find((m) => m.name === selectedName) ?? null;

  // The three ways to have no memories are different states, and an empty list
  // says none of them: a store that cannot be read is not a store with nothing
  // in it.
  let meta = t('shell.loading');
  if (error) meta = '';
  else if (payload?.unavailable) meta = t('memory.unavailable', { reason: payload.unavailable });
  else if (payload) {
    const count = t('memory.count', { count: memories.length });
    meta = t('memory.meta', { count, chars: payload.index_chars, budget: payload.index_budget });
  }

  return (
    <div className="info-memory">
      {meta && <p className="diff-drawer__meta">{meta}</p>}
      {payload?.directory && <p className="diff-drawer__meta">{payload.directory}</p>}

      {skipped.map((s) => (
        <p key={s.file} className="diff-drawer__error">
          {t('memory.skipped', { file: s.file, reason: s.reason })}
        </p>
      ))}

      {error ? (
        <p className="diff-drawer__error">{error}</p>
      ) : payload && memories.length === 0 ? (
        <p className="diff-drawer__empty">{t('memory.empty')}</p>
      ) : (
        <div className="diff-drawer__body">
          <aside className="diff-drawer__sidebar" aria-label={t('memory.list')}>
            {memories.map((m) => (
              <button
                key={m.name}
                type="button"
                className={`diff-drawer__file ${selected?.name === m.name ? 'diff-drawer__file--active' : ''}`}
                onClick={() => setSelectedName(m.name)}
                title={m.description}
              >
                <span className="diff-drawer__file-path">
                  <span className="diff-drawer__file-name">{m.name}</span>
                  <span className="diff-drawer__file-dir">{m.type} · {m.description}</span>
                </span>
              </button>
            ))}
          </aside>

          <section className="diff-drawer__viewer" aria-label={t('memory.viewer')}>
            {selected ? (
              <>
                <div className="diff-drawer__viewer-header">
                  <span className="diff-drawer__viewer-path">{selected.name}</span>
                  <span className="diff-drawer__viewer-kind">{selected.type}</span>
                  {formatWritten(selected, t, locale) && (
                    <span className="diff-drawer__viewer-kind">{formatWritten(selected, t, locale)}</span>
                  )}
                </div>
                <pre
                  className="diff-code"
                  style={{ whiteSpace: 'pre-wrap', overflow: 'auto', padding: '0.5rem' }}
                >
                  {selected.body}
                </pre>
              </>
            ) : (
              <p className="diff-drawer__empty">{t('memory.select')}</p>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
