import { useCallback, useEffect, useRef, useState } from 'react';
import { EventsOn } from '../lib/wailsRuntime';
import { useT } from '../i18n';

const EV_JOB_UPDATE = 'desktop/job-update';

function jobAge(job) {
  const start = Date.parse(job.created_at);
  const end = job.ended_at ? Date.parse(job.ended_at) : Date.now();
  if (Number.isNaN(start) || Number.isNaN(end)) return '';
  const secs = Math.max(0, Math.round((end - start) / 1000));
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m${secs % 60}s`;
  return `${Math.floor(mins / 60)}h${mins % 60}m`;
}

// A state the drawer does not know is shown as the Go side named it.
const STATE_KEY = {
  starting: 'jobs.state.starting',
  running: 'jobs.state.running',
  succeeded: 'jobs.state.succeeded',
  failed: 'jobs.state.failed',
  canceled: 'jobs.state.canceled',
};

function stateLabel(job, t) {
  if (job.state === 'canceled' && job.stop_reason) return t('jobs.state.canceledWith', { reason: job.stop_reason });
  if (job.state === 'failed' && job.error) return t('jobs.state.failedWith', { error: job.error });
  return STATE_KEY[job.state] ? t(STATE_KEY[job.state]) : job.state;
}

// JobsDrawer lists the project's background jobs and shows one job's output.
// It reuses the diff-drawer layout: same shell, list pane, and content pane.
export function JobsDrawer({ projectID, app, onClose }) {
  const t = useT();
  const [jobs, setJobs] = useState(null);
  const [error, setError] = useState(null);
  const [selectedID, setSelectedID] = useState('');
  const [output, setOutput] = useState('');
  const drawerRef = useRef(null);

  const refresh = useCallback(() => {
    app.ListJobs(projectID)
      .then((list) => {
        setJobs(list ?? []);
        setSelectedID((cur) => cur || (list?.[0]?.id ?? ''));
      })
      .catch((err) => setError(err?.message ?? String(err)));
  }, [projectID, app]);

  useEffect(() => {
    refresh();
    const unsub = EventsOn(EV_JOB_UPDATE, (payload) => {
      if (payload?.project_id === projectID) refresh();
    });
    return unsub;
  }, [projectID, refresh]);

  const selected = (jobs ?? []).find((j) => j.id === selectedID) ?? null;
  const selectedJobID = selected?.id ?? '';
  const selectedRunning = !!selected?.running;

  // Load output for the selected job; keep polling while it runs.
  useEffect(() => {
    if (!selectedJobID) { setOutput(''); return undefined; }
    let cancelled = false;
    const read = () => {
      app.GetJobOutput(projectID, selectedJobID, 'stdout', 0)
        .then((res) => { if (!cancelled) setOutput(res?.data ?? ''); })
        .catch(() => {});
    };
    read();
    const timer = selectedRunning ? setInterval(read, 2000) : null;
    return () => { cancelled = true; if (timer) clearInterval(timer); };
  }, [projectID, app, selectedJobID, selectedRunning]);

  useEffect(() => {
    function onKey(e) { if (e.key === 'Escape') onClose(); }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [onClose]);

  useEffect(() => { drawerRef.current?.focus(); }, []);

  const list = jobs ?? [];
  return (
    <div
      ref={drawerRef}
      className="diff-drawer"
      role="dialog"
      aria-modal="true"
      aria-label={t('jobs.dialog')}
      tabIndex={-1}
    >
      <div className="diff-drawer__header">
        <div>
          <h2 className="diff-drawer__title">{t('jobs.title')}</h2>
          <p className="diff-drawer__meta">
            {jobs
              ? t('jobs.meta', { count: list.length })
              : t('shell.loading')}
          </p>
        </div>
        <button type="button" className="diff-drawer__close" onClick={onClose} aria-label={t('shell.close')}>×</button>
      </div>

      {error ? (
        <p className="diff-drawer__error">{error}</p>
      ) : jobs && list.length === 0 ? (
        <p className="diff-drawer__empty">{t('jobs.empty')}</p>
      ) : (
        <div className="diff-drawer__body">
          <aside className="diff-drawer__sidebar" aria-label={t('jobs.list')}>
            {list.map((job) => (
              <button
                key={job.id}
                type="button"
                className={`diff-drawer__file ${selected?.id === job.id ? 'diff-drawer__file--active' : ''}`}
                onClick={() => setSelectedID(job.id)}
                title={job.command}
              >
                <span className="diff-drawer__file-path">
                  <span className="diff-drawer__file-name">{job.command}</span>
                  <span className="diff-drawer__file-dir">{stateLabel(job, t)} · {jobAge(job)}</span>
                </span>
              </button>
            ))}
          </aside>

          <section className="diff-drawer__viewer" aria-label={t('jobs.output')}>
            {selected ? (
              <>
                <div className="diff-drawer__viewer-header">
                  <span className="diff-drawer__viewer-path">{selected.id}</span>
                  <span className="diff-drawer__viewer-kind">{stateLabel(selected, t)}</span>
                  {selected.running && (
                    <button
                      type="button"
                      className="diff-drawer__close"
                      onClick={() => app.StopJob(projectID, selected.id).catch(() => {})}
                      title={t('jobs.stopTitle')}
                    >
                      {t('jobs.stop')}
                    </button>
                  )}
                </div>
                {output ? (
                  <pre className="diff-code" style={{ whiteSpace: 'pre-wrap', overflow: 'auto', padding: '0.5rem' }}>{output}</pre>
                ) : (
                  <p className="diff-drawer__empty">{t('jobs.noOutput')}</p>
                )}
              </>
            ) : (
              <p className="diff-drawer__empty">{t('jobs.select')}</p>
            )}
          </section>
        </div>
      )}
    </div>
  );
}
