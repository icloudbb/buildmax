import { useCallback, useEffect, useMemo, useState } from 'react';
import { EventsOn } from '../lib/wailsRuntime';
import { InfoModal, ConfirmModal } from './Modals';
import { ChatSession } from './ChatSession';
import { useLocale } from '@buildmax/gui';
import { useT } from '../i18n';
import { intlLocale } from '../lib/format';

const EV_SCHEDULE_UPDATE = 'desktop/schedule-update';

const emptyForm = { workingDir: '', name: '', prompt: '', model: '', cronExpr: '0 9 * * *', timezone: 'UTC' };

function formatWhen(value, locale) {
  if (!value) return '—';
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleString(intlLocale(locale), {
    month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit',
  });
}

// runStatusText turns a stored run status into a short label. A run left
// "running" is one the app did not see finish — it was closed mid-run.
function runStatusText(status, t) {
  if (status === 'ok' || status === 'failed' || status === 'running') return t(`schedules.status.${status}`);
  return status || '—';
}

// ScheduleRunDetail shows one fired run's conversation in the full chat UI — the
// same ChatSession the project chat uses, so it carries the model picker, context
// gauge, and slash commands — and a reply continues the session. Scheduled runs
// carry no project (they are hosted by the task's directory), so ChatSession is
// given an empty projectId and the backend resolves the host from the session.
function ScheduleRunDetail({ app, run, onClose }) {
  const t = useT();
  const { locale } = useLocale();
  const sessionId = run?.session_id || '';
  return (
    <InfoModal title={run?.schedule_name || t('schedules.run')} onClose={onClose} className="info-modal-panel--wide">
      <p className="info-modal__muted">
        {formatWhen(run?.fired_at, locale)} · {runStatusText(run?.status, t)}
      </p>
      {run?.error && <p className="info-modal__error">{run.error}</p>}
      <div className="schedule-run-chat">
        <ChatSession
          projectId=""
          projectName={t('schedules.scheduledProject')}
          defaultWorkspace={run?.working_dir || ''}
          sessions={[]}
          tab={{ kind: 'chat', ref: sessionId, sessionId, key: `chat:${sessionId}` }}
          app={app}
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
  const t = useT();
  const { locale } = useLocale();
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
  const [models, setModels] = useState([]);
  const [defaultModel, setDefaultModel] = useState('');

  const [openRunID, setOpenRunID] = useState(null);
  // The task awaiting a delete confirmation, shown in an in-app ConfirmModal
  // rather than window.confirm, which the native webview can silently drop.
  const [pendingDelete, setPendingDelete] = useState(null);

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

  // Load the available models when the form opens, for the model picker. The
  // list is the same one the chat offers; an empty choice means the default.
  useEffect(() => {
    if (!formOpen || !app?.GetSlashModels) return;
    app.GetSlashModels('', '')
      .then((res) => { setModels(res?.models ?? []); setDefaultModel(res?.current ?? ''); })
      .catch(() => { setModels([]); setDefaultModel(''); });
  }, [formOpen, app]);

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
      model: task.model ?? '',
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
        const cur = (tasks ?? []).find((task) => task.id === editingID);
        await app.UpdateScheduledTask(
          editingID, form.workingDir, form.name, form.prompt, form.cronExpr, form.timezone, form.model,
          cur ? cur.enabled : true,
        );
      } else {
        await app.CreateScheduledTask(form.workingDir, form.name, form.prompt, form.cronExpr, form.timezone, form.model);
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
        task.id, task.working_dir, task.name, task.prompt, task.cron_expr, task.timezone, task.model, !task.enabled,
      );
      refresh();
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  };

  const toggleAll = async () => {
    if (!app?.SetAllScheduledTasksEnabled) return;
    // Pause every task when any is enabled; otherwise enable them all.
    const target = !(tasks ?? []).some((task) => task.enabled);
    try {
      await app.SetAllScheduledTasksEnabled(target);
      refresh();
    } catch (err) {
      setError(err?.message ?? String(err));
    }
  };

  const confirmDelete = async () => {
    const task = pendingDelete;
    if (!task) return;
    try {
      await app.DeleteScheduledTask(task.id);
      if (editingID === task.id) closeForm();
      refresh();
    } catch (err) {
      setError(err?.message ?? String(err));
    } finally {
      setPendingDelete(null);
    }
  };

  const list = tasks ?? [];
  const runList = runs ?? [];
  // The task name is bold inside the sentence, so the sentence is split around
  // its placeholder rather than formatted into one string.
  const [deleteBefore, deleteAfter] = t('schedules.deleteConfirm').split('{name}');
  const activeRun = useMemo(() => (runs ?? []).find((r) => r.id === openRunID) ?? null, [runs, openRunID]);

  return (
    <div className="page-schedules">
      <div className="page-schedules__header">
        <div>
          <h1 className="page-schedules__title">{t('schedules.title')}</h1>
          {/* A scheduled task fires only while the desktop app is open; this note
              sets that expectation so nobody counts on an overnight run. */}
          <p className="page-schedules__subtitle">{t('schedules.openAppNote')}</p>
        </div>
        <div className="page-schedules__header-actions">
          {list.length > 0 && app?.SetAllScheduledTasksEnabled && (
            <button type="button" className="page-schedules__ghost" onClick={toggleAll}>
              {list.some((task) => task.enabled) ? t('schedules.pauseAll') : t('schedules.enableAll')}
            </button>
          )}
          <button type="button" className="page-schedules__primary" onClick={openCreate}>
            {t('schedules.new')}
          </button>
        </div>
      </div>

      {error && (
        <div className="page-schedules__banner page-schedules__banner--error">
          <span>{error}</span>
          <button type="button" onClick={() => setError(null)} aria-label={t('shell.dismiss')}>✕</button>
        </div>
      )}

      <div className="page-schedules__grid">
        <section className="page-schedules__section" aria-label={t('schedules.recentRuns')}>
          <div className="page-schedules__section-head">
            <h2>{t('schedules.recentRuns')}</h2>
          </div>
          {!runs ? (
            <p className="page-schedules__empty">{t('shell.loading')}</p>
          ) : runList.length === 0 ? (
            <p className="page-schedules__empty">{t('schedules.noRuns')}</p>
          ) : (
            <div className="page-schedules__runs">
              {runList.map((run) => (
                <button
                  key={run.id}
                  type="button"
                  className="page-schedules__run"
                  onClick={() => run.session_id && setOpenRunID(run.id)}
                  disabled={!run.session_id}
                  title={run.session_id ? t('schedules.openRun') : t('schedules.runNoSession')}
                >
                  <span className="page-schedules__run-title">{run.schedule_name || t('schedules.task')}</span>
                  <span className="page-schedules__run-meta">
                    <span>{formatWhen(run.fired_at, locale)}</span>
                    <span className={`page-schedules__run-status page-schedules__run-status--${run.status}`}>
                      {runStatusText(run.status, t)}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          )}
        </section>

        <section className="page-schedules__section" aria-label={t('schedules.tasksRegion')}>
          <div className="page-schedules__section-head">
            <h2>{tasks ? t('schedules.tasksCount', { count: list.length }) : t('schedules.tasks')}</h2>
          </div>
          {!tasks ? (
            <p className="page-schedules__empty">{t('shell.loading')}</p>
          ) : list.length === 0 ? (
            <p className="page-schedules__empty">{t('schedules.noTasks')}</p>
          ) : (
            <div className="page-schedules__list">
              {list.map((task) => (
                <div key={task.id} className="page-schedules__card">
                  <div className="page-schedules__card-head">
                    <span className="page-schedules__card-title">{task.name || task.prompt || t('schedules.task')}</span>
                    <span className={`page-schedules__badge ${task.enabled ? 'page-schedules__badge--on' : 'page-schedules__badge--off'}`}>
                      {task.enabled ? t('schedules.enabled') : t('schedules.paused')}
                    </span>
                    {task.consecutive_failures > 0 && (
                      <span className="page-schedules__badge page-schedules__badge--warn" title={t('schedules.failuresTitle')}>
                        {t('schedules.failures', { count: task.consecutive_failures })}
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
                    <span>{t('schedules.next', { when: formatWhen(task.next_fire_at, locale) })}</span>
                    <span>·</span>
                    <span>{t('schedules.last', { when: formatWhen(task.last_fire_at, locale) })}</span>
                  </div>
                  <div className="page-schedules__card-actions">
                    <button type="button" className="page-schedules__ghost" onClick={() => toggle(task)}>
                      {task.enabled ? t('schedules.pause') : t('schedules.resume')}
                    </button>
                    <button type="button" className="page-schedules__ghost" onClick={() => openEdit(task)}>
                      {t('schedules.edit')}
                    </button>
                    <button type="button" className="page-schedules__danger" onClick={() => setPendingDelete(task)}>
                      {t('schedules.delete')}
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      {formOpen && (
        <InfoModal title={editingID ? t('schedules.form.editTitle') : t('schedules.form.newTitle')} onClose={closeForm}>
          <form className="page-schedules__form" onSubmit={submit}>
            <label className="page-schedules__field">
              <span>{t('schedules.form.workingDir')} <em>{t('schedules.form.workingDirHint')}</em></span>
              <div className="page-schedules__dir-row">
                <input
                  type="text"
                  value={form.workingDir}
                  placeholder={t('schedules.form.workingDirPlaceholder')}
                  onChange={(e) => setForm((f) => ({ ...f, workingDir: e.target.value }))}
                />
                {app?.PickScheduleDir && (
                  <button type="button" className="page-schedules__ghost" onClick={browse}>{t('schedules.form.browse')}</button>
                )}
              </div>
            </label>
            <label className="page-schedules__field">
              <span>{t('schedules.form.name')} <em>{t('schedules.form.optional')}</em></span>
              <input
                type="text"
                value={form.name}
                placeholder={t('schedules.form.namePlaceholder')}
                onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
              />
            </label>
            <label className="page-schedules__field">
              <span>{t('schedules.form.model')}</span>
              <select
                value={form.model}
                onChange={(e) => setForm((f) => ({ ...f, model: e.target.value }))}
              >
                <option value="">{defaultModel ? t('schedules.form.defaultModel', { model: defaultModel }) : t('schedules.form.default')}</option>
                {models.map((m) => (
                  <option key={m.name} value={m.name}>
                    {m.provider_model && m.provider_model !== m.name ? `${m.name} — ${m.provider_model}` : m.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="page-schedules__field">
              <span>{t('schedules.form.prompt')}</span>
              <textarea
                rows={3}
                value={form.prompt}
                placeholder={t('schedules.form.promptPlaceholder')}
                onChange={(e) => setForm((f) => ({ ...f, prompt: e.target.value }))}
              />
            </label>
            <div className="page-schedules__field-row">
              <label className="page-schedules__field">
                <span>{t('schedules.form.cron')}</span>
                <input
                  type="text"
                  value={form.cronExpr}
                  placeholder="0 9 * * *"
                  onChange={(e) => setForm((f) => ({ ...f, cronExpr: e.target.value }))}
                />
              </label>
              <label className="page-schedules__field">
                <span>{t('schedules.form.timezone')}</span>
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
                  <span className="page-schedules__preview-label">{t('schedules.form.nextRuns')}</span>
                  <ul>
                    {preview.map((when) => <li key={when}>{formatWhen(when, locale)}</li>)}
                  </ul>
                </>
              ) : (
                <span className="page-schedules__preview-hint">
                  {t('schedules.form.cronHint')}
                </span>
              )}
            </div>
            {formError && <p className="page-schedules__form-error">{formError}</p>}
            <div className="modal-footer">
              <button type="button" className="modal-btn modal-btn--cancel" onClick={closeForm} disabled={saving}>
                {t('shell.cancel')}
              </button>
              <button type="submit" className="modal-btn modal-btn--primary" disabled={saving}>
                {editingID ? t('schedules.form.save') : t('schedules.form.create')}
              </button>
            </div>
          </form>
        </InfoModal>
      )}

      {activeRun && (
        <ScheduleRunDetail app={app} run={activeRun} onClose={() => setOpenRunID(null)} />
      )}

      {pendingDelete && (
        <ConfirmModal
          title={t('schedules.deleteTitle')}
          confirmLabel={t('schedules.deleteConfirmLabel')}
          onCancel={() => setPendingDelete(null)}
          onConfirm={confirmDelete}
          message={(
            <>
              <p className="confirm-modal__message">
                {deleteBefore}<strong>{pendingDelete.name || pendingDelete.prompt || t('schedules.thisTask')}</strong>{deleteAfter}
              </p>
              <p className="confirm-modal__message page-schedules__confirm-warn">
                {t('schedules.deleteWarn')}
              </p>
            </>
          )}
        />
      )}
    </div>
  );
}
