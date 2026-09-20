package desktop

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/config"
	schedstore "github.com/icloudbb/buildmax/internal/infra/localschedulestore"
	"github.com/icloudbb/buildmax/internal/service/schedule"
	"github.com/icloudbb/buildmax/internal/util"
)

// eventScheduleUpdate tells the frontend a scheduled task changed — created,
// edited, deleted, or fired — so it re-reads the list (and the session list, to
// show a session a fire just created). It carries no payload: the view refetches.
const eventScheduleUpdate = "desktop/schedule-update"

// schedulePollInterval is how often the resident loop checks for due tasks. One
// minute matches the smallest cron granularity (see internal/service/schedule).
const schedulePollInterval = time.Minute

// maxScheduleFailures pauses a task after this many consecutive fires that could
// not start a run, so a task pointed at a deleted project or an unreachable model
// does not retry forever. Mirrors the server dispatcher's runaway guard.
const maxScheduleFailures = 5

// ScheduledTaskPayload is one local scheduled task as the frontend sees it.
// Times are RFC3339 UTC strings, empty when unset.
type ScheduledTaskPayload struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	ProjectID           string `json:"project_id"`
	Prompt              string `json:"prompt"`
	CronExpr            string `json:"cron_expr"`
	Timezone            string `json:"timezone"`
	Enabled             bool   `json:"enabled"`
	NextFireAt          string `json:"next_fire_at,omitempty"`
	LastFireAt          string `json:"last_fire_at,omitempty"`
	LastSessionID       string `json:"last_session_id,omitempty"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

func scheduledTaskPayload(r schedstore.Record) ScheduledTaskPayload {
	p := ScheduledTaskPayload{
		ID:                  r.ID,
		Name:                r.Name,
		ProjectID:           r.ProjectID,
		Prompt:              r.Prompt,
		CronExpr:            r.CronExpr,
		Timezone:            r.Timezone,
		Enabled:             r.Enabled,
		LastSessionID:       r.LastSessionID,
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

// ListScheduledTasks returns every local scheduled task, oldest first.
func (a *App) ListScheduledTasks() ([]ScheduledTaskPayload, error) {
	rows, err := a.ensureScheduleStore().List()
	if err != nil {
		return nil, err
	}
	out := make([]ScheduledTaskPayload, len(rows))
	for i, r := range rows {
		out[i] = scheduledTaskPayload(r)
	}
	return out, nil
}

// CreateScheduledTask records a new task that runs prompt in projectID on the
// cron expression, in the given IANA timezone (default UTC). It is enabled and
// its first fire is the next cron slot after now.
func (a *App) CreateScheduledTask(projectID, name, prompt, cronExpr, timezone string) (ScheduledTaskPayload, error) {
	projectID = strings.TrimSpace(projectID)
	prompt = strings.TrimSpace(prompt)
	cronExpr = strings.TrimSpace(cronExpr)
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if projectID == "" {
		return ScheduledTaskPayload{}, fmt.Errorf("a project is required")
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
	if _, err := projectManager().Store().Get(context.Background(), projectID); err != nil {
		return ScheduledTaskPayload{}, fmt.Errorf("unknown project %q: %w", projectID, err)
	}
	now := time.Now().UTC()
	next, err := schedule.Next(cronExpr, timezone, now)
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
		ProjectID:  projectID,
		Prompt:     prompt,
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

// UpdateScheduledTask edits a task and enables or pauses it. Re-enabling or
// changing the cron/timezone recomputes the next fire from now; re-enabling also
// clears the failure count so a previously paused task gets a fresh runway.
func (a *App) UpdateScheduledTask(id, name, prompt, cronExpr, timezone string, enabled bool) (ScheduledTaskPayload, error) {
	prompt = strings.TrimSpace(prompt)
	cronExpr = strings.TrimSpace(cronExpr)
	timezone = strings.TrimSpace(timezone)
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
	now := time.Now().UTC()
	next, err := schedule.Next(cronExpr, timezone, now)
	if err != nil {
		return ScheduledTaskPayload{}, err
	}
	updated, err := a.ensureScheduleStore().Update(id, func(rec *schedstore.Record) {
		cronChanged := rec.CronExpr != cronExpr || rec.Timezone != timezone
		reEnabled := enabled && !rec.Enabled
		rec.Name = strings.TrimSpace(name)
		rec.Prompt = prompt
		rec.CronExpr = cronExpr
		rec.Timezone = timezone
		rec.Enabled = enabled
		if reEnabled {
			rec.ConsecutiveFailures = 0
		}
		if enabled && (cronChanged || reEnabled) {
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

// DeleteScheduledTask removes a task. Sessions it already created are kept.
func (a *App) DeleteScheduledTask(id string) error {
	if err := a.ensureScheduleStore().Delete(id); err != nil {
		return err
	}
	a.emitScheduleUpdate()
	return nil
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

// fireScheduled starts one run of a task in a new session, as a background turn:
// it does not touch project recency (the user did not reach for the project) and
// records the created session id back onto the task when the run starts. The
// returned error is a failure to start the run (project or model unavailable),
// which the runaway guard counts.
func (a *App) fireScheduled(ctx context.Context, r schedstore.Record, key string) error {
	store := a.ensureScheduleStore()
	id := r.ID
	lc := &desktopRun{
		app:           a,
		ctx:           ctx,
		projectID:     r.ProjectID,
		sessionID:     "",
		key:           key,
		touchLastUsed: false,
		onStart: func(sess *agentapp.SessionContext) {
			if _, err := store.Update(id, func(rec *schedstore.Record) { rec.LastSessionID = sess.ID() }); err != nil {
				slog.Warn("record scheduled session failed", "task", id, "err", err)
			}
			a.emitScheduleUpdate()
		},
	}
	_, err := a.scheduler.Submit(ctx, key, "", r.Prompt, a.hostForProject(r.ProjectID, lc), lc)
	return err
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
