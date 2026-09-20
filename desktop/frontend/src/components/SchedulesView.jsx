import { useCallback, useEffect, useMemo, useState } from 'react';
import { EventsOn } from '../lib/wailsRuntime';

const EV_SCHEDULE_UPDATE = 'desktop/schedule-update';

// A scheduled task fires only while the desktop app is open; this note sets that
// expectation so nobody counts on an overnight run with the app closed.
const OPEN_APP_NOTE = 'Scheduled tasks run only while this app is open.';

function formatWhen(value) {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString(undefined, {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

const emptyForm = { projectID: '', name: '', prompt: '', cronExpr: '0 9 * * *', timezone: 'UTC' };

// SchedulesView is the first-class Schedules surface reached from the sidebar. It
// lists every local scheduled task across projects and lets the user create,
// edit, pause, resume, and delete them.
export function SchedulesView({ app, projects, onOpenSession }) {
  const [tasks, setTasks] = useState(null);
  const [error, setError] = useState(null);
  const [formError, setFormError] = useState(null);
  const [form, setForm] = useState(emptyForm);
  const [editingID, setEditingID] = useState(null);
  const [saving, setSaving] = useState(false);

  const refresh = useCallback(() => {
    if (!app?.ListScheduledTasks) { setTasks([]); return; }
    app.ListScheduledTasks()
      .then((list) => setTasks(list ?? []))
      .catch((err) => setError(err?.message ?? String(err)));
  }, [app]);

  useEffect(() => {
    refresh();
    const unsub = EventsOn(EV_SCHEDULE_UPDATE, () => refresh());
    return unsub;
  }, [refresh]);

  const projectName = useMemo(() => {
    const map = new Map();
    for (const p of projects ?? []) map.set(p.id, p.name);
    return (id) => map.get(id) || 'Unknown project';
  }, [projects]);

  const resetForm = () => {
    setForm({ ...emptyForm, projectID: projects?.[0]?.id ?? '' });
    setEditingID(null);
    setFormError(null);
  };

  // Seed the project selector once projects arrive and no edit is in progress.
  useEffect(() => {
    if (!editingID && !form.projectID && projects?.length) {
      setForm((f) => ({ ...f, projectID: projects[0].id }));
    }
  }, [projects, editingID, form.projectID]);

  const startEdit = (task) => {
    setEditingID(task.id);
    setFormError(null);
    setForm({
      projectID: task.project_id,
      name: task.name ?? '',
      prompt: task.prompt ?? '',
      cronExpr: task.cron_expr ?? '',
      timezone: task.timezone ?? 'UTC',
    });
  };

  const submit = async (e) => {
    e.preventDefault();
    setFormError(null);
    setSaving(true);
    try {
      if (editingID) {
        const cur = (tasks ?? []).find((t) => t.id === editingID);
        await app.UpdateScheduledTask(
          editingID, form.name, form.prompt, form.cronExpr, form.timezone,
          cur ? cur.enabled : true,
        );
      } else {
        await app.CreateScheduledTask(form.projectID, form.name, form.prompt, form.cronExpr, form.timezone);
      }
      resetForm();
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
        task.id, task.name, task.prompt, task.cron_expr, task.timezone, !task.enabled,
      );
      refresh();
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  };

  const remove = async (task) => {
    if (!window.confirm(`Delete scheduled task${task.name ? ` “${task.name}”` : ''}? Sessions it already created are kept.`)) return;
    try {
      await app.DeleteScheduledTask(task.id);
      if (editingID === task.id) resetForm();
      refresh();
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  };

  const list = tasks ?? [];
  const noProjects = (projects ?? []).length === 0;

  return (
    <div className="page-schedules">
      <div className="page-schedules__header">
        <div>
          <h1 className="page-schedules__title">Schedules</h1>
          <p className="page-schedules__subtitle">{OPEN_APP_NOTE}</p>
        </div>
      </div>

      {error && (
        <div className="page-schedules__banner page-schedules__banner--error">
          <span>{error}</span>
          <button type="button" onClick={() => setError(null)} aria-label="Dismiss">✕</button>
        </div>
      )}

      <div className="page-schedules__grid">
        <section className="page-schedules__section" aria-label={editingID ? 'Edit scheduled task' : 'New scheduled task'}>
          <div className="page-schedules__section-head">
            <h2>{editingID ? 'Edit task' : 'New task'}</h2>
          </div>
          {noProjects ? (
            <p className="page-schedules__empty">Open a project first, then schedule a task in it.</p>
          ) : (
            <form className="page-schedules__form" onSubmit={submit}>
              <label className="page-schedules__field">
                <span>Project</span>
                <select
                  value={form.projectID}
                  disabled={!!editingID}
                  onChange={(e) => setForm((f) => ({ ...f, projectID: e.target.value }))}
                >
                  {(projects ?? []).map((p) => (
                    <option key={p.id} value={p.id}>{p.name}</option>
                  ))}
                </select>
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
              {formError && <p className="page-schedules__form-error">{formError}</p>}
              <div className="page-schedules__form-actions">
                <button type="submit" className="page-schedules__primary" disabled={saving}>
                  {editingID ? 'Save changes' : 'Create task'}
                </button>
                {editingID && (
                  <button type="button" className="page-schedules__ghost" onClick={resetForm} disabled={saving}>
                    Cancel
                  </button>
                )}
              </div>
              <p className="page-schedules__hint">
                Standard 5-field cron (minute hour day month weekday), evaluated in the timezone.
              </p>
            </form>
          )}
        </section>

        <section className="page-schedules__section" aria-label="Scheduled tasks">
          <div className="page-schedules__section-head">
            <h2>Tasks {tasks ? `(${list.length})` : ''}</h2>
          </div>
          {!tasks ? (
            <p className="page-schedules__empty">Loading…</p>
          ) : list.length === 0 ? (
            <p className="page-schedules__empty">No scheduled tasks yet.</p>
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
                    <span>{projectName(task.project_id)}</span>
                    <span>·</span>
                    <span><code>{task.cron_expr}</code> {task.timezone}</span>
                  </div>
                  <div className="page-schedules__card-meta">
                    <span>Next: {formatWhen(task.next_fire_at)}</span>
                    <span>·</span>
                    <span>Last: {formatWhen(task.last_fire_at)}</span>
                    {task.last_session_id && onOpenSession && (
                      <>
                        <span>·</span>
                        <button
                          type="button"
                          className="page-schedules__link"
                          onClick={() => onOpenSession(task.last_session_id)}
                        >
                          Open last run
                        </button>
                      </>
                    )}
                  </div>
                  <div className="page-schedules__card-actions">
                    <button type="button" className="page-schedules__ghost" onClick={() => toggle(task)}>
                      {task.enabled ? 'Pause' : 'Resume'}
                    </button>
                    <button type="button" className="page-schedules__ghost" onClick={() => startEdit(task)}>
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
    </div>
  );
}
