package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	histstore "github.com/icloudbb/buildmax/internal/infra/localschedulehistorystore"
	schedstore "github.com/icloudbb/buildmax/internal/infra/localschedulestore"
	"github.com/icloudbb/buildmax/internal/service/schedule"
	"github.com/icloudbb/buildmax/internal/util"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// eventScheduleUpdate tells the frontend a scheduled task changed — created,
// edited, deleted, or fired — so it re-reads the list (and the session list, to
// show a session a fire just created). It carries no payload: the view refetches.
const eventScheduleUpdate = "desktop/schedule-update"

// schedulePollInterval is how often the resident loop checks for due tasks. One
// minute matches the smallest cron granularity (see internal/service/schedule).
const schedulePollInterval = time.Minute

// maxScheduleFailures pauses a task after this many consecutive fires that could
// not start a run, so a task pointed at an unreachable model or a vanished
// directory does not retry forever. Mirrors the server dispatcher's runaway guard.
const maxScheduleFailures = 5

// schedulePreviewCount is how many upcoming fire times PreviewScheduledTask
// returns, enough for the create form to show the cron expression's cadence.
const schedulePreviewCount = 5

// ScheduledTaskPayload is one local scheduled task as the frontend sees it.
// Times are RFC3339 UTC strings, empty when unset.
type ScheduledTaskPayload struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	WorkingDir          string `json:"working_dir"`
	Prompt              string `json:"prompt"`
	Model               string `json:"model"`
	CronExpr            string `json:"cron_expr"`
	Timezone            string `json:"timezone"`
	Enabled             bool   `json:"enabled"`
	NextFireAt          string `json:"next_fire_at,omitempty"`
	LastFireAt          string `json:"last_fire_at,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	// ModelProblem says why Model cannot run in the app's current mode, so the
	// card can say so before the next fire: one of the modelProblem codes, empty
	// when it can run. ModelServer names the signed-in deployment for
	// modelProblemNotOffered, empty in local mode.
	ModelProblem string `json:"model_problem,omitempty"`
	ModelServer  string `json:"model_server,omitempty"`
}

// Why a task's model cannot run now. The view words each one; the fire's own
// error says the same in English for the run history.
const (
	// modelProblemMissing is a task saved before tasks pinned their model.
	modelProblemMissing = "no_model"
	// modelProblemNotOffered is a model the current mode does not offer, such as
	// a settings.yaml model after signing in.
	modelProblemNotOffered = "not_offered"
	// modelProblemUnchecked means the current mode's models could not be listed.
	modelProblemUnchecked = "unchecked"
)

// scheduleModels is what a task's model is checked against: a runtime of the
// mode the app is in now.
type scheduleModels interface {
	DefaultModelName() string
	OffersModel(name string) bool
	ManagedServerURL() string
}

// checkScheduleModel reports whether a task's stored model can run under
// models, as a modelProblem code and the error a fire fails with. It never
// substitutes another model: unattended work that cannot run on the model it
// was saved with fails and says why.
func checkScheduleModel(model string, models scheduleModels) (string, error) {
	if model == "" {
		return modelProblemMissing, errors.New("this schedule has no model: it was saved before schedules kept their model. Edit it and choose one")
	}
	if models.OffersModel(model) {
		return "", nil
	}
	if url := models.ManagedServerURL(); url != "" {
		return modelProblemNotOffered, fmt.Errorf("model %q is not offered by %s, which this app is signed in to. Edit the schedule to choose one of its models, or sign out to use this machine's settings.yaml", model, url)
	}
	return modelProblemNotOffered, fmt.Errorf("model %q is not in this machine's settings.yaml. Edit the schedule to choose a configured model, or sign in to the deployment that offers it", model)
}

// scheduleModelHost is the runtime the Schedules view reads the current mode's
// models from. It is the one GetSlashModels("", "") resolves, so the form's
// "Default (X)" is X when the task is saved.
func (a *App) scheduleModelHost() (*agentapp.AgentApp, error) {
	return a.resolveSessionApp("", "")
}

// pinScheduleModel is the model a task is saved with: the one chosen, or for
// "Default" (empty) the default of this moment, so the task keeps running the
// model the person saw rather than whatever the default later becomes.
func (a *App) pinScheduleModel(model string) (string, error) {
	if model != "" {
		return model, nil
	}
	host, err := a.scheduleModelHost()
	if err != nil {
		return "", fmt.Errorf("resolve the default model: %w", err)
	}
	name := host.DefaultModelName()
	if name == "" {
		return "", errors.New("no model is configured to run this schedule on")
	}
	return name, nil
}

func scheduledTaskPayload(r schedstore.Record) ScheduledTaskPayload {
	p := ScheduledTaskPayload{
		ID:                  r.ID,
		Name:                r.Name,
		WorkingDir:          r.WorkingDir,
		Prompt:              r.Prompt,
		Model:               r.Model,
		CronExpr:            r.CronExpr,
		Timezone:            r.Timezone,
		Enabled:             r.Enabled,
		ConsecutiveFailures: r.ConsecutiveFailures,
		CreatedAt:           r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:           r.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if !r.NextFireAt.IsZero() {
		p.NextFireAt = r.NextFireAt.UTC().Format(time.RFC3339)
	}
	if !r.LastFireAt.IsZero() {
		p.LastFireAt = r.LastFireAt.UTC().Format(time.RFC3339)
	}
	return p
}

// ScheduleRunPayload is one fire of a scheduled task as the frontend sees it,
// the history the Schedules view lists and links to a session transcript.
type ScheduleRunPayload struct {
	ID           string `json:"id"`
	ScheduleID   string `json:"schedule_id"`
	ScheduleName string `json:"schedule_name"`
	// WorkingDir is the owning task's directory, so continuing the run's session
	// (ContinueScheduleRun) can reach the same AgentApp without another lookup.
	WorkingDir string `json:"working_dir"`
	SessionID  string `json:"session_id,omitempty"`
	Model      string `json:"model,omitempty"`
	FiredAt    string `json:"fired_at"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
}

// ensureScheduleStore lazily opens the local scheduled-task store. It resolves
// the path only on first use so an App built in a test that never touches
// schedules does not require BUILDMAX_HOME.
func (a *App) ensureScheduleStore() *schedstore.FileStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.schedules == nil {
		a.schedules = schedstore.NewFileStore(config.ScheduledTasksPath())
	}
	return a.schedules
}

// ensureScheduleRunStore lazily opens the scheduled-task run history store, on
// the same first-use terms as ensureScheduleStore.
func (a *App) ensureScheduleRunStore() *histstore.FileStore {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scheduleRuns == nil {
		a.scheduleRuns = histstore.NewFileStore(config.ScheduleRunsPath())
	}
	return a.scheduleRuns
}

// ListScheduledTasks returns every local scheduled task, oldest first, each
// saying whether its model can run in the current mode.
func (a *App) ListScheduledTasks() ([]ScheduledTaskPayload, error) {
	rows, err := a.ensureScheduleStore().List()
	if err != nil {
		return nil, err
	}
	out := make([]ScheduledTaskPayload, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	host, hostErr := a.scheduleModelHost()
	for i, r := range rows {
		p := scheduledTaskPayload(r)
		switch {
		case r.Model == "":
			p.ModelProblem = modelProblemMissing
		case hostErr != nil:
			p.ModelProblem = modelProblemUnchecked
		default:
			p.ModelProblem, _ = checkScheduleModel(r.Model, host)
			if p.ModelProblem == modelProblemNotOffered {
				p.ModelServer = strings.TrimPrefix(strings.TrimPrefix(host.ManagedServerURL(), "https://"), "http://")
			}
		}
		out[i] = p
	}
	return out, nil
}

// CreateScheduledTask records a new task that runs prompt in workingDir on the
// cron expression, in the given IANA timezone (default UTC). An empty workingDir
// means the user's home directory. It is enabled and its first fire is the next
// cron slot after now.
func (a *App) CreateScheduledTask(workingDir, name, prompt, cronExpr, timezone, model string) (ScheduledTaskPayload, error) {
	prompt = strings.TrimSpace(prompt)
	cronExpr = strings.TrimSpace(cronExpr)
	timezone = strings.TrimSpace(timezone)
	model = strings.TrimSpace(model)
	if timezone == "" {
		timezone = "UTC"
	}
	if prompt == "" {
		return ScheduledTaskPayload{}, fmt.Errorf("a prompt is required")
	}
	if cronExpr == "" {
		return ScheduledTaskPayload{}, fmt.Errorf("a cron expression is required")
	}
	if err := schedule.Validate(cronExpr, timezone); err != nil {
		return ScheduledTaskPayload{}, err
	}
	dir, err := resolveWorkingDir(workingDir)
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	now := time.Now().UTC()
	next, err := schedule.Next(cronExpr, timezone, now)
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	model, err = a.pinScheduleModel(model)
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	id, err := util.NewPublicID()
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	r := schedstore.Record{
		ID:         id,
		Name:       strings.TrimSpace(name),
		WorkingDir: dir,
		Prompt:     prompt,
		Model:      model,
		CronExpr:   cronExpr,
		Timezone:   timezone,
		Enabled:    true,
		NextFireAt: next,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := a.ensureScheduleStore().Add(r); err != nil {
		return ScheduledTaskPayload{}, err
	}
	a.emitScheduleUpdate()
	return scheduledTaskPayload(r), nil
}

// UpdateScheduledTask edits a task's fields. An empty model is "Default" chosen
// in the form, pinned to the default of this moment as on create. Changing the
// cron or timezone of an enabled task recomputes its next fire from now.
// Pausing and resuming is SetScheduledTaskEnabled, so it never touches the model.
func (a *App) UpdateScheduledTask(id, workingDir, name, prompt, cronExpr, timezone, model string) (ScheduledTaskPayload, error) {
	prompt = strings.TrimSpace(prompt)
	cronExpr = strings.TrimSpace(cronExpr)
	timezone = strings.TrimSpace(timezone)
	model = strings.TrimSpace(model)
	if timezone == "" {
		timezone = "UTC"
	}
	if prompt == "" {
		return ScheduledTaskPayload{}, fmt.Errorf("a prompt is required")
	}
	if cronExpr == "" {
		return ScheduledTaskPayload{}, fmt.Errorf("a cron expression is required")
	}
	if err := schedule.Validate(cronExpr, timezone); err != nil {
		return ScheduledTaskPayload{}, err
	}
	dir, err := resolveWorkingDir(workingDir)
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	now := time.Now().UTC()
	next, err := schedule.Next(cronExpr, timezone, now)
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	model, err = a.pinScheduleModel(model)
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	updated, err := a.ensureScheduleStore().Update(id, func(rec *schedstore.Record) {
		cronChanged := rec.CronExpr != cronExpr || rec.Timezone != timezone
		rec.Name = strings.TrimSpace(name)
		rec.WorkingDir = dir
		rec.Prompt = prompt
		rec.Model = model
		rec.CronExpr = cronExpr
		rec.Timezone = timezone
		if rec.Enabled && cronChanged {
			rec.NextFireAt = next
		}
		rec.UpdatedAt = now
	})
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	a.emitScheduleUpdate()
	return scheduledTaskPayload(updated), nil
}

// setScheduleEnabled enables or pauses one task. Enabling recomputes its next
// fire from now and clears its failure count, so a paused task gets a fresh
// runway; pausing just stops it.
func setScheduleEnabled(rec *schedstore.Record, enabled bool, now time.Time) {
	rec.Enabled = enabled
	if enabled {
		rec.ConsecutiveFailures = 0
		if next, err := schedule.Next(rec.CronExpr, rec.Timezone, now); err == nil {
			rec.NextFireAt = next
		}
	}
	rec.UpdatedAt = now
}

// SetScheduledTaskEnabled pauses or resumes one task without touching anything
// else about it, its model included.
func (a *App) SetScheduledTaskEnabled(id string, enabled bool) (ScheduledTaskPayload, error) {
	now := time.Now().UTC()
	updated, err := a.ensureScheduleStore().Update(id, func(rec *schedstore.Record) {
		if rec.Enabled != enabled {
			setScheduleEnabled(rec, enabled, now)
		}
	})
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	a.emitScheduleUpdate()
	return scheduledTaskPayload(updated), nil
}

// SetAllScheduledTasksEnabled enables or pauses every task in one call, for the
// Schedules view's one-click toggle, each as SetScheduledTaskEnabled would.
// Tasks already in the target state are left untouched. Returns the updated list.
func (a *App) SetAllScheduledTasksEnabled(enabled bool) ([]ScheduledTaskPayload, error) {
	store := a.ensureScheduleStore()
	rows, err := store.List()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	changed := false
	for _, r := range rows {
		if r.Enabled == enabled {
			continue
		}
		if _, err := store.Update(r.ID, func(rec *schedstore.Record) {
			setScheduleEnabled(rec, enabled, now)
		}); err != nil {
			return nil, err
		}
		changed = true
	}
	if changed {
		a.emitScheduleUpdate()
	}
	return a.ListScheduledTasks()
}

// DeleteScheduledTask removes a task, its run history, and the projectless
// sessions those runs created — the sessions are reachable only through this
// task's history, so nothing else keeps them.
func (a *App) DeleteScheduledTask(id string) error {
	if err := a.ensureScheduleStore().Delete(id); err != nil {
		return err
	}
	sessions, err := a.ensureScheduleRunStore().DeleteBySchedule(id)
	if err != nil {
		slog.Warn("delete scheduled task history failed", "task", id, "err", err)
	}
	for _, sid := range sessions {
		if err := sessionManager().Delete(sid); err != nil {
			slog.Warn("delete scheduled session failed", "task", id, "session", sid, "err", err)
		}
	}
	a.emitScheduleUpdate()
	return nil
}

// PickScheduleDir opens a native directory picker and returns the chosen path,
// or "" if the user cancels. It lets the create form set a working directory
// without typing the full path.
func (a *App) PickScheduleDir() (string, error) {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return "", fmt.Errorf("app not ready")
	}
	return wruntime.OpenDirectoryDialog(ctx, wruntime.OpenDialogOptions{Title: "Choose a working directory"})
}

// PreviewScheduledTask returns the next few UTC fire times of a cron expression
// in a timezone, so the create form can show the cadence as the user types. It
// validates the expression and timezone, returning the same error as a save.
func (a *App) PreviewScheduledTask(cronExpr, timezone string) ([]string, error) {
	cronExpr = strings.TrimSpace(cronExpr)
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if cronExpr == "" {
		return nil, fmt.Errorf("a cron expression is required")
	}
	if err := schedule.Validate(cronExpr, timezone); err != nil {
		return nil, err
	}
	out := make([]string, 0, schedulePreviewCount)
	after := time.Now().UTC()
	for range schedulePreviewCount {
		next, err := schedule.Next(cronExpr, timezone, after)
		if err != nil {
			return nil, err
		}
		out = append(out, next.UTC().Format(time.RFC3339))
		after = next
	}
	return out, nil
}

// ListScheduleRuns returns the run history across every task, newest first, with
// each task's display name resolved so the view need not join it. Older runs are
// pruned per task by the store.
func (a *App) ListScheduleRuns() ([]ScheduleRunPayload, error) {
	runs, err := a.ensureScheduleRunStore().List()
	if err != nil {
		return nil, err
	}
	tasks, err := a.ensureScheduleStore().List()
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(tasks))
	dirs := make(map[string]string, len(tasks))
	for _, t := range tasks {
		names[t.ID] = scheduleDisplayName(t)
		dirs[t.ID] = t.WorkingDir
	}
	out := make([]ScheduleRunPayload, 0, len(runs))
	for _, r := range runs {
		out = append(out, ScheduleRunPayload{
			ID:           r.ID,
			ScheduleID:   r.ScheduleID,
			ScheduleName: names[r.ScheduleID],
			WorkingDir:   dirs[r.ScheduleID],
			SessionID:    r.SessionID,
			Model:        r.Model,
			FiredAt:      r.FiredAt.UTC().Format(time.RFC3339),
			Status:       r.Status,
			Error:        r.Error,
		})
	}
	return out, nil
}

// resolveWorkingDir returns the directory a task runs in: the user's home when
// blank, else the given path once confirmed to be an existing directory.
func resolveWorkingDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home directory: %w", err)
		}
		return home, nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("working directory %q: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("working directory %q is not a directory", dir)
	}
	return dir, nil
}

// scheduleDisplayName is the task's name, or the first line of its prompt when
// unnamed, so the history list never shows a blank label.
func scheduleDisplayName(r schedstore.Record) string {
	if name := strings.TrimSpace(r.Name); name != "" {
		return name
	}
	line := strings.TrimSpace(r.Prompt)
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if len(line) > 60 {
		line = line[:60] + "…"
	}
	if line == "" {
		return "Task"
	}
	return line
}

func (a *App) emitScheduleUpdate() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	a.emit(ctx, eventScheduleUpdate, struct{}{})
}

// --- Firing loop ---

// startScheduler launches the resident tick loop. It fires once immediately so a
// task that came due while the app was closed runs once on launch (coalesced),
// then checks each minute. Called from Startup, when the live context exists.
func (a *App) startScheduler() {
	a.ensureScheduleStore()
	stop := make(chan struct{})
	a.mu.Lock()
	if a.stopSched != nil {
		a.mu.Unlock()
		return
	}
	a.stopSched = stop
	a.mu.Unlock()
	go a.scheduleLoop(stop)
}

// stopScheduler ends the tick loop. Safe to call when it never started.
func (a *App) stopScheduler() {
	a.mu.Lock()
	stop := a.stopSched
	a.stopSched = nil
	a.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

func (a *App) scheduleLoop(stop <-chan struct{}) {
	a.sweepSchedules()
	t := time.NewTicker(schedulePollInterval)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			a.sweepSchedules()
		case <-stop:
			return
		}
	}
}

// sweepSchedules fires every enabled task whose next fire is due, then advances
// it. A task whose previous fire is still running is skipped for this occurrence
// and advanced, so a long run does not let fires pile up.
func (a *App) sweepSchedules() {
	a.mu.Lock()
	ctx := a.ctx
	a.mu.Unlock()
	if ctx == nil {
		return
	}
	store := a.ensureScheduleStore()
	rows, err := store.List()
	if err != nil {
		slog.Warn("list scheduled tasks failed", "err", err)
		return
	}
	now := time.Now().UTC()
	fired := false
	for _, r := range rows {
		if !r.Enabled || r.NextFireAt.After(now) {
			continue
		}
		key := scheduleRunKey(r.ID)
		if a.scheduler.Busy(key) {
			if _, err := store.Update(r.ID, func(rec *schedstore.Record) {
				recomputeNext(rec, now)
				rec.UpdatedAt = now
			}); err != nil {
				slog.Warn("advance busy scheduled task failed", "task", r.ID, "err", err)
			}
			fired = true
			continue
		}
		fireErr := a.fireScheduled(ctx, r, key)
		if _, err := store.Update(r.ID, func(rec *schedstore.Record) {
			applyFire(rec, now, fireErr)
		}); err != nil {
			slog.Warn("record scheduled fire failed", "task", r.ID, "err", err)
		}
		fired = true
	}
	if fired {
		a.emitScheduleUpdate()
	}
}

// scheduleRunKey serializes a task's own fires (a fire waits for the previous one
// of the same task) while staying clear of interactive run keys (runKey), so a
// scheduled fire never queues behind a user's new chat and vice versa.
func scheduleRunKey(id string) string { return "schedule\x00" + id }

// fireScheduled starts one run of a task in a new, projectless session in the
// task's working directory, as a background turn: it does not touch project
// recency and records the fire in the task's run history — a row at start with
// the created session, stamped with how the run ended. The run uses the task's
// stored model; when the current mode does not offer it, the fire fails with
// that reason rather than running on another model. The returned error is a
// failure to start the run (directory or model unavailable), which the runaway
// guard counts; that failure is recorded too, so an attempt always leaves a row.
func (a *App) fireScheduled(ctx context.Context, r schedstore.Record, key string) error {
	runs := a.ensureScheduleRunStore()
	id := r.ID
	firedAt := time.Now().UTC()
	runID, err := util.NewPublicID()
	if err != nil {
		return err
	}
	// The run never started, so onStart never recorded it; leave a failed row so
	// the attempt still shows in history.
	failed := func(err error) error {
		if addErr := runs.Add(histstore.Run{
			ID:         runID,
			ScheduleID: id,
			Model:      r.Model,
			FiredAt:    firedAt,
			Status:     histstore.StatusFailed,
			Error:      err.Error(),
		}); addErr != nil {
			slog.Warn("record failed scheduled run failed", "task", id, "err", addErr)
		}
		a.emitScheduleUpdate()
		return err
	}
	dir := strings.TrimSpace(r.WorkingDir)
	if dir == "" {
		if home, herr := os.UserHomeDir(); herr == nil {
			dir = home
		}
	}
	host, err := a.agentAppForDir(dir)
	if err != nil {
		return failed(err)
	}
	if _, err := checkScheduleModel(r.Model, host); err != nil {
		return failed(err)
	}
	lc := &desktopRun{
		app:           a,
		ctx:           ctx,
		sessionID:     "",
		key:           key,
		touchLastUsed: false,
		onStart: func(sess *agentapp.SessionContext) {
			// OnStart runs before the turn reads the model (resolveRunContext), so
			// setting it on the open session here makes this fire use the task's
			// pinned model, never the app default.
			sess.SetModel(r.Model)
			if err := runs.Add(histstore.Run{
				ID:         runID,
				ScheduleID: id,
				SessionID:  sess.ID(),
				Model:      r.Model,
				FiredAt:    firedAt,
				Status:     histstore.StatusRunning,
			}); err != nil {
				slog.Warn("record scheduled run failed", "task", id, "err", err)
			}
			a.emitScheduleUpdate()
		},
		onDone: func(runErr error) {
			status, msg := histstore.StatusOK, ""
			if runErr != nil {
				status, msg = histstore.StatusFailed, runErr.Error()
			}
			if _, err := runs.Update(runID, func(run *histstore.Run) {
				run.Status = status
				run.Error = msg
			}); err != nil {
				slog.Warn("record scheduled run outcome failed", "task", id, "err", err)
			}
			a.emitScheduleUpdate()
		},
	}
	if _, err := a.scheduler.Submit(ctx, key, "", r.Prompt, a.hostForDir(dir), lc); err != nil {
		return failed(err)
	}
	return nil
}

// recomputeNext advances a task to its next fire after `after`, or pauses it if
// its stored cron no longer parses (which write-time validation should prevent).
func recomputeNext(rec *schedstore.Record, after time.Time) {
	next, err := schedule.Next(rec.CronExpr, rec.Timezone, after)
	if err != nil {
		rec.Enabled = false
		return
	}
	rec.NextFireAt = next
}

// applyFire records the outcome of one fire: it stamps the fire time, advances to
// the next slot, and either clears the failure count or increments it, pausing
// the task once it reaches maxScheduleFailures.
func applyFire(rec *schedstore.Record, now time.Time, fireErr error) {
	rec.LastFireAt = now
	if fireErr != nil {
		rec.ConsecutiveFailures++
		if rec.ConsecutiveFailures >= maxScheduleFailures {
			rec.Enabled = false
		}
	} else {
		rec.ConsecutiveFailures = 0
	}
	recomputeNext(rec, now)
	rec.UpdatedAt = now
}
