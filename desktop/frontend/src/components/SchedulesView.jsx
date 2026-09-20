import { useCallback, useEffect, useMemo, useState } from 'react';
import { EventsOn } from '../lib/wailsRuntime';
import { InfoModal } from './Modals';
import { ChatSession } from './ChatSession';

const EV_SCHEDULE_UPDATE = 'desktop/schedule-update';

// A scheduled task fires only while the desktop app is open; this note sets that
// expectation so nobody counts on an overnight run with the app closed.
const OPEN_APP_NOTE = 'Scheduled tasks run only while this app is open.';

const emptyForm = { workingDir: '', name: '', prompt: '', cronExpr: '0 9 * * *', timezone: 'UTC' };

function formatWhen(value) {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

// runStatusText turns a stored run status into a short label. A run left
// "running" is one the app did not see finish — it was closed mid-run.
function runStatusText(status) {
  if (status === 'ok') return 'Done';
  if (status === 'failed') return 'Failed';
  if (status === 'running') return 'Running';
  return status || '—';
}

// ScheduleRunDetail shows one fired run's conversation in the full chat UI — the
// same ChatSession the project chat uses, so it carries the model picker, context
// gauge, and slash commands — and a reply continues the session. Scheduled runs
// carry no project (they are hosted by the task's directory), so ChatSession is
// given an empty projectId and the backend resolves the host from the session.
function ScheduleRunDetail({ app, run, onClose }) {
  const sessionId = run?.session_id || '';
  return (
    <InfoModal title={run?.schedule_name || 'Run'} onClose={onClose}>
      <p className="info-modal__muted">
        {formatWhen(run?.fired_at)} · {runStatusText(run?.status)}
      </p>
      {run?.error && <p className="info-modal__error">{run.error}</p>}
      <div className="schedule-run-chat">
        <ChatSession
          projectId=""
          projectName="Scheduled"
          defaultWorkspace={run?.working_dir || ''}
          sessions={[]}
          tab={{ kind: 'chat', ref: sessionId, sessionId, key: `chat:${sessionId}` }}
          app={app}
          approvalRequest={null}
          onRespond={() => {}}
          onSessionAdopted={() => {}}
          onSessionsChanged={() => {}}
          onTitle={() => {}}
          onOpenSession={() => {}}
          onShowChanges={() => {}}
        />
      </div>
    </InfoModal>
  );
}

// SchedulesView is the first-class Schedules surface reached from the sidebar.
// Left column: recent run history, each opening its conversation. Right column:
// the scheduled tasks themselves. Creating a task happens in a modal behind the
// New Schedule button, not an always-visible form.
export function SchedulesView({ app }) {
  const [tasks, setTasks] = useState(null);
  const [runs, setRuns] = useState(null);
  const [error, setError] = useState(null);

  const [formOpen, setFormOpen] = useState(false);
  const [editingID, setEditingID] = useState(null);
  const [form, setForm] = useState(emptyForm);
  const [formError, setFormError] = useState(null);
  const [saving, setSaving] = useState(false);
  const [preview, setPreview] = useState(null);
  const [previewError, setPreviewError] = useState(null);

  const [openRunID, setOpenRunID] = useState(null);

  const refresh = useCallback(() => {
    if (!app?.ListScheduledTasks) { setTasks([]); setRuns([]); return; }
    app.ListScheduledTasks()
      .then((list) => setTasks(list ?? []))
      .catch((err) => setError(err?.message ?? String(err)));
    if (app.ListScheduleRuns) {
      app.ListScheduleRuns()
        .then((list) => setRuns(list ?? []))
        .catch((err) => setError(err?.message ?? String(err)));
    } else {
      setRuns([]);
    }
  }, [app]);

  useEffect(() => {
    refresh();
    const unsub = EventsOn(EV_SCHEDULE_UPDATE, () => refresh());
    return unsub;
  }, [refresh]);

  // Preview the cron expression's next fires as the user types, so the cadence
  // is visible before saving. Debounced, and only while the form is open.
  useEffect(() => {
    if (!formOpen || !app?.PreviewScheduledTask) return undefined;
    const cron = form.cronExpr.trim();
    if (!cron) { setPreview(null); setPreviewError(null); return undefined; }
    const timer = setTimeout(() => {
      app.PreviewScheduledTask(cron, form.timezone)
        .then((list) => { setPreview(list ?? []); setPreviewError(null); })
        .catch((err) => { setPreview(null); setPreviewError(err?.message ?? String(err)); });
    }, 300);
    return () => clearTimeout(timer);
  }, [app, formOpen, form.cronExpr, form.timezone]);

  const openCreate = () => {
    setEditingID(null);
    setForm(emptyForm);
    setFormError(null);
    setPreview(null);
    setPreviewError(null);
    setFormOpen(true);
  };

  const openEdit = (task) => {
    setEditingID(task.id);
    setForm({
      workingDir: task.working_dir ?? '',
      name: task.name ?? '',
      prompt: task.prompt ?? '',
      cronExpr: task.cron_expr ?? '',
      timezone: task.timezone ?? 'UTC',
    });
    setFormError(null);
    setPreview(null);
    setPreviewError(null);
    setFormOpen(true);
  };

  const closeForm = () => {
    setFormOpen(false);
    setEditingID(null);
    setForm(emptyForm);
    setFormError(null);
    setPreview(null);
    setPreviewError(null);
  };

  const browse = async () => {
    if (!app?.PickScheduleDir) return;
    try {
      const dir = await app.PickScheduleDir();
      if (dir) setForm((f) => ({ ...f, workingDir: dir }));
    } catch (err) {
      setFormError(err?.message ?? String(err));
    }
  };

  const submit = async (e) => {
    e.preventDefault();
    setFormError(null);
    setSaving(true);
    try {
      if (editingID) {
        const cur = (tasks ?? []).find((t) => t.id === editingID);
        await app.UpdateScheduledTask(
          editingID, form.workingDir, form.name, form.prompt, form.cronExpr, form.timezone,
          cur ? cur.enabled : true,
        );
      } else {
        await app.CreateScheduledTask(form.workingDir, form.name, form.prompt, form.cronExpr, form.timezone);
      }
      closeForm();
      refresh();
    } catch (err) {
      setFormError(err?.message ?? String(err));
    } finally {
      setSaving(false);
    }
  };

  const toggle = async (task) => {
    try {
      await app.UpdateScheduledTask(
        task.id, task.working_dir, task.name, task.prompt, task.cron_expr, task.timezone, !task.enabled,
      );
      refresh();
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  };

  const remove = async (task) => {
    if (!window.confirm(
      `Delete scheduled task${task.name ? ` “${task.name}”` : ''}? Its run history and the sessions it created are removed too.`,
    )) return;
    try {
      await app.DeleteScheduledTask(task.id);
      if (editingID === task.id) closeForm();
      refresh();
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  };

  const list = tasks ?? [];
  const runList = runs ?? [];
  const activeRun = useMemo(() => (runs ?? []).find((r) => r.id === openRunID) ?? null, [runs, openRunID]);

  return (
    <div className="page-schedules">
      <div className="page-schedules__header">
        <div>
          <h1 className="page-schedules__title">Schedules</h1>
          <p className="page-schedules__subtitle">{OPEN_APP_NOTE}</p>
        </div>
        <button type="button" className="page-schedules__primary" onClick={openCreate}>
          New Schedule
        </button>
      </div>

      {error && (
        <div className="page-schedules__banner page-schedules__banner--error">
          <span>{error}</span>
          <button type="button" onClick={() => setError(null)} aria-label="Dismiss">✕</button>
        </div>
      )}

      <div className="page-schedules__grid">
        <section className="page-schedules__section" aria-label="Recent runs">
          <div className="page-schedules__section-head">
            <h2>Recent runs</h2>
          </div>
          {!runs ? (
            <p className="page-schedules__empty">Loading…</p>
          ) : runList.length === 0 ? (
            <p className="page-schedules__empty">No runs yet. A task's fires will appear here.</p>
          ) : (
            <div className="page-schedules__runs">
              {runList.map((run) => (
                <button
                  key={run.id}
                  type="button"
                  className="page-schedules__run"
                  onClick={() => run.session_id && setOpenRunID(run.id)}
                  disabled={!run.session_id}
                  title={run.session_id ? 'Open this run' : 'This run created no session'}
                >
                  <span className="page-schedules__run-title">{run.schedule_name || 'Task'}</span>
                  <span className="page-schedules__run-meta">
                    <span>{formatWhen(run.fired_at)}</span>
                    <span className={`page-schedules__run-status page-schedules__run-status--${run.status}`}>
                      {runStatusText(run.status)}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </section>

        <section className="page-schedules__section" aria-label="Scheduled tasks">
          <div className="page-schedules__section-head">
            <h2>Tasks {tasks ? `(${list.length})` : ''}</h2>
          </div>
          {!tasks ? (
            <p className="page-schedules__empty">Loading…</p>
          ) : list.length === 0 ? (
            <p className="page-schedules__empty">No scheduled tasks yet. Create one with New Schedule.</p>
          ) : (
            <div className="page-schedules__list">
              {list.map((task) => (
                <div key={task.id} className="page-schedules__card">
                  <div className="page-schedules__card-head">
                    <span className="page-schedules__card-title">{task.name || task.prompt || 'Task'}</span>
                    <span className={`page-schedules__badge ${task.enabled ? 'page-schedules__badge--on' : 'page-schedules__badge--off'}`}>
                      {task.enabled ? 'Enabled' : 'Paused'}
                    </span>
                    {task.consecutive_failures > 0 && (
                      <span className="page-schedules__badge page-schedules__badge--warn" title="Consecutive failed fires">
                        {task.consecutive_failures} failed
                      </span>
                    )}
                  </div>
                  <div className="page-schedules__card-meta">
                    <code className="page-schedules__dir" title={task.working_dir}>{task.working_dir}</code>
                  </div>
                  <div className="page-schedules__card-meta">
                    <span><code>{task.cron_expr}</code> {task.timezone}</span>
                  </div>
                  <div className="page-schedules__card-meta">
                    <span>Next: {formatWhen(task.next_fire_at)}</span>
                    <span>·</span>
                    <span>Last: {formatWhen(task.last_fire_at)}</span>
                  </div>
                  <div className="page-schedules__card-actions">
                    <button type="button" className="page-schedules__ghost" onClick={() => toggle(task)}>
                      {task.enabled ? 'Pause' : 'Resume'}
                    </button>
                    <button type="button" className="page-schedules__ghost" onClick={() => openEdit(task)}>
                      Edit
                    </button>
                    <button type="button" className="page-schedules__ghost page-schedules__ghost--danger" onClick={() => remove(task)}>
                      Delete
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      {formOpen && (
        <InfoModal title={editingID ? 'Edit schedule' : 'New schedule'} onClose={closeForm}>
          <form className="page-schedules__form" onSubmit={submit}>
            <label className="page-schedules__field">
              <span>Working directory <em>(optional — defaults to your home)</em></span>
              <div className="page-schedules__dir-row">
                <input
                  type="text"
                  value={form.workingDir}
                  placeholder="~ (home)"
                  onChange={(e) => setForm((f) => ({ ...f, workingDir: e.target.value }))}
                />
                {app?.PickScheduleDir && (
                  <button type="button" className="page-schedules__ghost" onClick={browse}>Browse…</button>
                )}
              </div>
            </label>
            <label className="page-schedules__field">
              <span>Name <em>(optional)</em></span>
              <input
                type="text"
                value={form.name}
                placeholder="Morning summary"
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              />
            </label>
            <label className="page-schedules__field">
              <span>Prompt</span>
              <textarea
                rows={3}
                value={form.prompt}
                placeholder="What should the agent do each time?"
                onChange={(e) => setForm((f) => ({ ...f, prompt: e.target.value }))}
              />
            </label>
            <div className="page-schedules__field-row">
              <label className="page-schedules__field">
                <span>Cron</span>
                <input
                  type="text"
                  value={form.cronExpr}
                  placeholder="0 9 * * *"
                  onChange={(e) => setForm((f) => ({ ...f, cronExpr: e.target.value }))}
                />
              </label>
              <label className="page-schedules__field">
                <span>Timezone</span>
                <input
                  type="text"
                  value={form.timezone}
                  placeholder="Asia/Shanghai"
                  onChange={(e) => setForm((f) => ({ ...f, timezone: e.target.value }))}
                />
              </label>
            </div>
            <div className="page-schedules__preview" aria-live="polite">
              {previewError ? (
                <span className="page-schedules__preview-error">{previewError}</span>
              ) : preview && preview.length > 0 ? (
                <>
                  <span className="page-schedules__preview-label">Next runs</span>
                  <ul>
                    {preview.map((when) => <li key={when}>{formatWhen(when)}</li>)}
                  </ul>
                </>
              ) : (
                <span className="page-schedules__preview-hint">
                  Standard 5-field cron (minute hour day month weekday), evaluated in the timezone.
                </span>
              )}
            </div>
            {formError && <p className="page-schedules__form-error">{formError}</p>}
            <div className="modal-footer">
              <button type="button" className="modal-btn modal-btn--cancel" onClick={closeForm} disabled={saving}>
                Cancel
              </button>
              <button type="submit" className="modal-btn modal-btn--primary" disabled={saving}>
                {editingID ? 'Save changes' : 'Create schedule'}
              </button>
            </div>
          </form>
        </InfoModal>
      )}

      {activeRun && (
        <ScheduleRunDetail app={app} run={activeRun} onClose={() => setOpenRunID(null)} />
      )}
    </div>
  );
}
