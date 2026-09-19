package desktop

import (
	"fmt"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/agentapp/job"
	"github.com/icloudbb/buildmax/internal/infra/proc"
)

// eventJobUpdate carries one background-job lifecycle transition to the
// frontend (payload JobPayload).
const eventJobUpdate = "desktop/job-update"

// eventJobDeliveryPending nudges the frontend that parked background events
// wait for a session (payload JobDeliveryPendingPayload). The frontend pulls
// them with DeliverNextJobEvent when that session is on screen and idle —
// the frontend is the only party that knows which session is on screen.
const eventJobDeliveryPending = "desktop/job-delivery-pending"

// eventJobDelivery announces that a delivery turn is starting (payload
// JobDeliveryPayload), so the transcript can say why the agent is speaking
// unprompted.
const eventJobDelivery = "desktop/job-delivery"

// JobDeliveryPendingPayload reports parked deliveries for one session.
type JobDeliveryPendingPayload struct {
	ProjectID string `json:"project_id"`
	SessionID string `json:"session_id"`
	Pending   int    `json:"pending"`
}

// JobDeliveryPayload describes the delivery turn that just started.
type JobDeliveryPayload struct {
	ProjectID string `json:"project_id"`
	SessionID string `json:"session_id"`
	JobID     string `json:"job_id"`
	Source    string `json:"source"`
	Title     string `json:"title"`
}

// JobPayload is one background job as the frontend sees it.
type JobPayload struct {
	ProjectID  string `json:"project_id"`
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	State      string `json:"state"`
	StopReason string `json:"stop_reason,omitempty"`
	ExitCode   int    `json:"exit_code,omitempty"`
	Error      string `json:"error,omitempty"`
	Command    string `json:"command"`
	SessionID  string `json:"session_id,omitempty"`
	Running    bool   `json:"running"`
	CreatedAt  string `json:"created_at"`
	EndedAt    string `json:"ended_at,omitempty"`
}

// JobOutputPayload is one incremental output read.
type JobOutputPayload struct {
	Data       string `json:"data"`
	NextCursor uint64 `json:"next_cursor"`
	Dropped    uint64 `json:"dropped,omitempty"`
	Running    bool   `json:"running"`
	State      string `json:"state"`
}

// desktopJobOutputBytes bounds one bridge read; the frontend pages with the
// returned cursor.
const desktopJobOutputBytes = 64 * 1024

func jobPayload(projectID string, j job.Job) JobPayload {
	p := JobPayload{
		ProjectID:  projectID,
		ID:         j.ID,
		Kind:       string(j.Kind),
		State:      string(j.State),
		StopReason: string(j.StopReason),
		ExitCode:   j.ExitCode,
		Error:      j.Err,
		Command:    j.Command,
		SessionID:  j.Provenance.SessionID,
		Running:    j.Running(),
		CreatedAt:  j.CreatedAt.Format(time.RFC3339),
	}
	if !j.EndedAt.IsZero() {
		p.EndedAt = j.EndedAt.Format(time.RFC3339)
	}
	return p
}

// pumpJobEvents forwards one AgentApp's job lifecycle events to the frontend
// and parks requested deliveries per owning session until the manager closes
// the subscription (when the app closes). Parking rather than pushing is what
// lets a delivery wait for its session to come back on screen instead of
// degrading to a notification.
func (a *App) pumpJobEvents(projectID string, m *job.Manager) {
	events, _ := m.Subscribe("")
	go func() {
		for e := range events {
			switch e.Type {
			case job.EventMonitorLine:
				if e.Job.Deliver && e.Line != "" {
					a.parkJobEvent(projectID, e.Job.Provenance.SessionID, agentapp.MonitorLineEvent(e))
				}
			default:
				a.emit(a.ctx, eventJobUpdate, jobPayload(projectID, e.Job))
				if !e.Job.Running() && e.Job.Deliver {
					a.parkJobEvent(projectID, e.Job.Provenance.SessionID, agentapp.CompletionEvent(m, e.Job))
				}
			}
		}
	}()
}

func deliveryKey(projectID, sessionID string) string { return projectID + "\x00" + sessionID }

// parkJobEvent queues one delivery for its owning session and nudges the
// frontend.
func (a *App) parkJobEvent(projectID, sessionID string, ev agentapp.BackgroundEvent) {
	key := deliveryKey(projectID, sessionID)
	a.mu.Lock()
	if a.pendingJobEvents == nil {
		a.pendingJobEvents = make(map[string][]agentapp.BackgroundEvent)
	}
	a.pendingJobEvents[key] = append(a.pendingJobEvents[key], ev)
	pending := len(a.pendingJobEvents[key])
	ctx := a.ctx
	a.mu.Unlock()
	a.emit(ctx, eventJobDeliveryPending, &JobDeliveryPendingPayload{
		ProjectID: projectID,
		SessionID: sessionID,
		Pending:   pending,
	})
}

// DeliverNextJobEvent runs one parked background event for the given session
// as its own streaming turn, mirroring SendMessageStream's serialization.
// It returns false when nothing was started: no parked event, or a run
// already in flight — the parked event then simply waits for the next
// trigger. The frontend calls it when the session is on screen and idle.
func (a *App) DeliverNextJobEvent(projectID, sessionID string) (bool, error) {
	if projectID == "" {
		return false, fmt.Errorf("project ID required")
	}
	key := deliveryKey(projectID, sessionID)
	a.mu.Lock()
	ctx := a.ctx
	pending := len(a.pendingJobEvents[key])
	a.mu.Unlock()
	if ctx == nil {
		return false, fmt.Errorf("app not ready")
	}
	if pending == 0 {
		return false, nil
	}

	// pop removes this session's oldest parked event. The scheduler calls it
	// under its own lock and only once it has won the run slot, so the event is
	// consumed exactly once even when two deliveries race — the reason the pop is
	// the scheduler's to time rather than done up front here.
	var delivered agentapp.BackgroundEvent
	pop := func() (agentapp.BackgroundEvent, bool) {
		a.mu.Lock()
		defer a.mu.Unlock()
		events := a.pendingJobEvents[key]
		if len(events) == 0 {
			return agentapp.BackgroundEvent{}, false
		}
		delivered = events[0]
		if len(events) == 1 {
			delete(a.pendingJobEvents, key)
		} else {
			a.pendingJobEvents[key] = events[1:]
		}
		return delivered, true
	}

	// A job delivery differs from a user turn only here: it announces the
	// delivery before its first output, and does not touch project recency,
	// because the user did not reach for the project.
	lc := &desktopRun{
		app:       a,
		ctx:       ctx,
		projectID: projectID,
		sessionID: sessionID,
		key:       runKey(projectID, sessionID),
		onStart: func(sess *agentapp.SessionContext) {
			a.emit(ctx, eventJobDelivery, &JobDeliveryPayload{
				ProjectID: projectID,
				SessionID: sess.ID(),
				JobID:     delivered.JobID,
				Source:    delivered.Source,
				Title:     delivered.Title,
			})
		},
	}
	return a.scheduler.StartEvent(ctx, runKey(projectID, sessionID), sessionID, a.hostForProject(projectID, lc), pop, lc)
}

// PendingJobDeliveries reports how many parked deliveries wait for a session.
func (a *App) PendingJobDeliveries(projectID, sessionID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pendingJobEvents[deliveryKey(projectID, sessionID)])
}

// jobsForProject resolves the project's job manager, or a clear error on a
// surface state where jobs are unavailable.
func (a *App) jobsForProject(projectID string) (*job.Manager, error) {
	ag, err := a.agentAppForProject(projectID)
	if err != nil {
		return nil, err
	}
	m := ag.Jobs()
	if m == nil {
		return nil, fmt.Errorf("background jobs are not available for this project")
	}
	return m, nil
}

// ListJobs returns the project's background jobs in creation order.
func (a *App) ListJobs(projectID string) ([]JobPayload, error) {
	m, err := a.jobsForProject(projectID)
	if err != nil {
		return nil, err
	}
	jobs := m.List()
	out := make([]JobPayload, len(jobs))
	for i, j := range jobs {
		out[i] = jobPayload(projectID, j)
	}
	return out, nil
}

// StopJob requests termination of one background job.
func (a *App) StopJob(projectID, jobID string) error {
	m, err := a.jobsForProject(projectID)
	if err != nil {
		return err
	}
	return m.Stop(jobID)
}

// GetJobOutput reads one job's captured output incrementally. stream is
// "stdout" (default) or "stderr"; pass the previous next_cursor to continue.
func (a *App) GetJobOutput(projectID, jobID, stream string, cursor uint64) (JobOutputPayload, error) {
	m, err := a.jobsForProject(projectID)
	if err != nil {
		return JobOutputPayload{}, err
	}
	j, ok := m.Get(jobID)
	if !ok {
		return JobOutputPayload{}, fmt.Errorf("no such job: %s", jobID)
	}
	s := proc.Stdout
	if stream == "stderr" {
		s = proc.Stderr
	}
	chunk, err := m.Output(jobID, s, cursor, desktopJobOutputBytes)
	if err != nil {
		return JobOutputPayload{}, err
	}
	return JobOutputPayload{
		Data:       string(chunk.Data),
		NextCursor: chunk.Next,
		Dropped:    chunk.Dropped,
		Running:    j.Running(),
		State:      string(j.State),
	}, nil
}
