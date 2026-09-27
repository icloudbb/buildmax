package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/agentapp/job"
	"github.com/icloudbb/buildmax/internal/core/agent"

	tea "charm.land/bubbletea/v2"
)

const slashJobsInlinePanelMaxLines = 12

// slashJobsPanelChromeLines are the panel's own lines: box border, title, the
// "… N more" row, the closing note, and the key hints.
const slashJobsPanelChromeLines = 9

// jobEventMsg carries one background-job lifecycle event into the update loop.
type jobEventMsg struct {
	Event job.Event
}

// listenJobsCmd waits for the next job event. It re-arms itself from the
// jobEventMsg handler, one event per Cmd, the same shape as the stream channel.
func listenJobsCmd(events <-chan job.Event) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		e, ok := <-events
		if !ok {
			return nil
		}
		return jobEventMsg{Event: e}
	}
}

// handleJobEvent prints terminal transitions to the scrollback, parks
// requested deliveries under their owning session, and re-arms the listener.
// Job starts are already visible as the launching tool's result, so only the
// ending — and a react monitor's lines — is news.
//
// A parked delivery drains only while its session is on screen and idle; the
// rest wait for their session to come back (see nextParkedJobEvent).
func handleJobEvent(m *Model, msg jobEventMsg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	if listen := listenJobsCmd(m.jobEvents); listen != nil {
		cmds = append(cmds, listen)
	}
	ev := msg.Event

	if ev.Type == job.EventMonitorLine {
		// Notify-only monitor lines never reach the transcript or the model;
		// /tasks and JobOutput are their surface. React lines wake the owner.
		if ev.Job.Deliver && ev.Line != "" {
			cmds = append(cmds, m.parkJobEvent(ev.Job, agentapp.MonitorLineEvent(ev))...)
		}
		return m, tea.Batch(cmds...)
	}

	if ev.Job.Running() {
		return m, tea.Batch(cmds...)
	}
	cmds = append(cmds, tea.Println(formatJobEventForScrollback(ev.Job)+"\n"))
	if ev.Job.Deliver {
		var jobs *job.Manager
		if m.opts.App != nil {
			jobs = m.opts.App.Jobs()
		}
		cmds = append(cmds, m.parkJobEvent(ev.Job, agentapp.CompletionEvent(jobs, ev.Job))...)
	}
	return m, tea.Batch(cmds...)
}

// ownsJob reports whether the job belongs to the session currently on screen.
func (m *Model) ownsJob(j job.Job) bool {
	return m.opts.Session != nil && j.Provenance.SessionID == m.opts.Session.ID()
}

// parkJobEvent queues one delivery under the job's owning session and, when
// that session is on screen and idle, asks for a drain.
func (m *Model) parkJobEvent(j job.Job, ev agentapp.BackgroundEvent) []tea.Cmd {
	if m.parkedJobEvents == nil {
		m.parkedJobEvents = make(map[string][]agentapp.BackgroundEvent)
	}
	owner := j.Provenance.SessionID
	m.parkedJobEvents[owner] = append(m.parkedJobEvents[owner], ev)
	if m.ownsJob(j) && !m.busy {
		return []tea.Cmd{drainQueueCmd()}
	}
	return nil
}

// nextParkedJobEvent pops the oldest delivery parked for the session on
// screen.
func (m *Model) nextParkedJobEvent() (agentapp.BackgroundEvent, bool) {
	if m.opts.Session == nil {
		return agentapp.BackgroundEvent{}, false
	}
	events := m.parkedJobEvents[m.opts.Session.ID()]
	if len(events) == 0 {
		return agentapp.BackgroundEvent{}, false
	}
	ev := events[0]
	if len(events) == 1 {
		delete(m.parkedJobEvents, m.opts.Session.ID())
	} else {
		m.parkedJobEvents[m.opts.Session.ID()] = events[1:]
	}
	return ev, true
}

// startBackgroundEventRun mirrors startRun for a background-event turn: same
// busy state, stream channel, and queue handoff, with a dim header saying why
// the agent is speaking unprompted.
func startBackgroundEventRun(m *Model, ev agentapp.BackgroundEvent) tea.Cmd {
	channel := beginRun(m)
	header := queuedMessageStyle.Render("⟳ " + ev.Source + " from " + ev.JobID + " — " + jobCommandSummary(ev.Title, 60))
	return tea.Sequence(
		tea.Println(header+"\n"),
		tea.Batch(
			tea.Tick(time.Duration(carouselTick)*time.Millisecond, func(time.Time) tea.Msg { return carouselTickMsg{} }),
			runBackgroundEventWithStream(m.runs, m.opts, ev, channel, m.queue),
		),
	)
}

// runBackgroundEventWithStream is runAgentWithStream's sibling for a
// background event turn.
func runBackgroundEventWithStream(owner *tuiRunOwner, opts TUIOpts, ev agentapp.BackgroundEvent, channel chan tea.Msg, queue *agent.MessageQueue) tea.Cmd {
	started := owner.Go(func(ctx context.Context) {
		defer close(channel)
		sink := &streamSinkToChannel{ctx: ctx, channel: channel}
		evSink := eventSinkToChannel(ctx, channel)
		result, err := opts.App.RunBackgroundEvent(ctx, opts.Session, ev, agentapp.RunPromptOpts{
			Stream:     sink,
			Approval:   opts.Approval,
			Questioner: opts.Questioner,
			EventSink:  evSink,
			Pending:    queue,
			Digest:     true,
		})
		sendTUIMessage(ctx, channel, agentDoneMsg{Result: result, Err: err})
	})
	if !started {
		close(channel)
	}
	return func() tea.Msg { return <-channel }
}

// formatJobEventForScrollback renders one finished job as a dim notice line:
// visible without pretending to be part of the conversation.
func formatJobEventForScrollback(j job.Job) string {
	glyph := "✓"
	if j.State != job.StateSucceeded {
		glyph = "✗"
	}
	detail := string(j.State)
	switch {
	case j.State == job.StateCanceled && j.StopReason != "":
		detail += " (" + string(j.StopReason) + ")"
	case j.State == job.StateFailed && j.Err != "":
		detail += " (" + j.Err + ")"
	}
	line := fmt.Sprintf("%s job %s %s after %s — %s", glyph, j.ID, detail, jobAge(j), jobCommandSummary(j.Command, 60))
	return queuedMessageStyle.Render(line)
}

// slashJobsPanel implements slashPanel for the /tasks overlay. It reads live
// state from the manager on every render, so an open panel tracks job
// progress without its own refresh plumbing.
type slashJobsPanel struct {
	selected int
	// offset is the first job the panel draws. The list is longer than the
	// terminal allows more often than it looks — one `s` on a row that was
	// never drawn stops a job the user cannot see — so the drawn rows are a
	// window that follows the selection rather than the first N.
	offset int
}

func openSlashJobs(m *Model) (tea.Model, tea.Cmd) {
	if m.opts.App == nil || m.opts.App.Jobs() == nil {
		m.err = "background jobs are not available on this surface"
		return m, nil
	}
	return m.openPanel(&slashJobsPanel{})
}

func (p *slashJobsPanel) jobs(m *Model) []job.Job {
	if m.opts.App == nil || m.opts.App.Jobs() == nil {
		return nil
	}
	return m.opts.App.Jobs().List()
}

func (p *slashJobsPanel) HandleKey(m *Model, msg tea.KeyPressMsg) (bool, tea.Cmd) {
	jobs := p.jobs(m)
	switch msg.Code {
	case tea.KeyEscape:
		_, cmd := m.closeActivePanel()
		return true, cmd
	case tea.KeyUp:
		if p.selected > 0 {
			p.selected--
			p.scrollIntoView(m, len(jobs))
		}
		return true, nil
	case tea.KeyDown:
		if p.selected < len(jobs)-1 {
			p.selected++
			p.scrollIntoView(m, len(jobs))
		}
		return true, nil
	}
	if msg.String() == "s" {
		if p.selected < len(jobs) && jobs[p.selected].Running() {
			_ = m.opts.App.Jobs().Stop(jobs[p.selected].ID)
		}
		return true, nil
	}
	return false, nil
}

func (p *slashJobsPanel) FooterHint() string {
	return "↑/↓: select · s: stop job · esc: close"
}

func (p *slashJobsPanel) OnClose(_ *Model) {}

// rowBudget is how many jobs the panel may draw.
func (p *slashJobsPanel) rowBudget(m *Model) int {
	return m.panelListBudget(slashJobsInlinePanelMaxLines, slashJobsPanelChromeLines)
}

// scrollIntoView moves the window the least it can to contain the selection.
func (p *slashJobsPanel) scrollIntoView(m *Model, total int) {
	rows := p.rowBudget(m)
	if p.selected < p.offset {
		p.offset = p.selected
	} else if p.selected >= p.offset+rows {
		p.offset = p.selected - rows + 1
	}
	p.clampOffset(total, rows)
}

// clampOffset keeps the window inside the list. Unlike the other panels this
// one lists live state: a job finishing does not shorten the list, but the
// manager's history does not grow forever, and the row budget shrinks when the
// terminal does under a panel that is already open and already scrolled.
func (p *slashJobsPanel) clampOffset(total, rows int) {
	if last := total - rows; p.offset > last {
		p.offset = last
	}
	if p.offset < 0 {
		p.offset = 0
	}
}

func (p *slashJobsPanel) Render(m *Model, maxLineWidth int) string {
	jobs := p.jobs(m)
	if p.selected >= len(jobs) && len(jobs) > 0 {
		p.selected = len(jobs) - 1
	}
	var b strings.Builder
	b.WriteString(slashPanelTitleStyle.Render("Background Jobs"))
	b.WriteString("\n\n")
	if len(jobs) == 0 {
		b.WriteString("No background jobs. Ask for a command with run_in_background to start one.")
		return strings.TrimRight(b.String(), "\n") + "\n\nesc: close"
	}
	rows := p.rowBudget(m)
	p.clampOffset(len(jobs), rows)
	end := min(p.offset+rows, len(jobs))
	for i := p.offset; i < end; i++ {
		j := jobs[i]
		marker := "  "
		if i == p.selected {
			marker = "> "
		}
		state := string(j.State)
		if j.State == job.StateCanceled && j.StopReason != "" {
			state += " (" + string(j.StopReason) + ")"
		}
		line := fmt.Sprintf("%s%s  %-9s  %6s  %s", marker, j.ID, state, jobAge(j), jobCommandSummary(j.Command, 40))
		b.WriteString(truncateRunes(line, maxLineWidth))
		b.WriteByte('\n')
	}
	if remaining := len(jobs) - end; remaining > 0 {
		fmt.Fprintf(&b, "… %d more\n", remaining)
	}
	b.WriteString("\nJobs share this workspace and stop when the app exits.")
	return strings.TrimRight(b.String(), "\n") + "\n\n↑/↓ select · s stop · esc close"
}

func jobAge(j job.Job) string {
	end := j.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	return end.Sub(j.CreatedAt).Round(time.Second).String()
}

func jobCommandSummary(cmd string, maxRunes int) string {
	cmd = strings.ReplaceAll(cmd, "\n", " ")
	return truncateRunes(cmd, maxRunes)
}
