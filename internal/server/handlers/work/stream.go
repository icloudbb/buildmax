package work

import (
	"bufio"
	"net/http"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/server/httputil"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
)

func (h *Handler) getChatStreamHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	taskID := r.PathValue("task_id")
	if taskID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "task_id required")
		return
	}
	_, _, ok = h.getTaskForSpace(w, r, spaceID, taskID)
	if !ok {
		return
	}
	if h.cfg.TaskRuns == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "task runs not configured")
		return
	}
	// Stream keys are the task_run_id, not the task: a task's turns each own a
	// distinct buffer, so a finished run's output is never replayed to the next
	// run's watchers. Resolve the run the client is here to watch.
	activeRun, err := h.cfg.TaskRuns.GetActiveTaskRunByTask(r.Context(), taskID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_chat_stream", "task_id", taskID)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	// Flush the headers now. Without this the response is buffered until the
	// first event, so a client watching a quiet run waits for the stream to
	// open instead of being told it is open and then waiting for output.
	if flusher != nil {
		flusher.Flush()
	}

	if activeRun == nil {
		// No run is producing output: the one the client saw finished between its
		// poll and this request. Say done so it reloads the authoritative record
		// rather than holding an idle connection open. The poll owns lifecycle.
		writeSSE(w, "done")
		if flusher != nil {
			flusher.Flush()
		}
		return
	}
	runID := activeRun.ID

	events, unsub := h.cfg.Hub.Subscribe(runID)
	defer unsub()

	// A run that is thinking between deltas emits nothing, but a proxy in front
	// of this handler closes an idle response on its read timeout, and the client
	// then reads that as a finished run. A periodic SSE comment keeps the stream
	// accountable through silence; the client's parser ignores a comment frame.
	heartbeat := time.NewTicker(httputil.SSEHeartbeatInterval)
	defer heartbeat.Stop()

	if buf := h.cfg.Hub.Buffer(runID); buf != "" {
		writeSSE(w, buf)
		if flusher != nil {
			flusher.Flush()
		}
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			writeSSEComment(w)
			if flusher != nil {
				flusher.Flush()
			}
		case <-h.cfg.Drain:
			// This server is going away and the run is not: it lives in the
			// database and keeps streaming into whichever instance the client
			// reconnects to. Saying so is what stops the client from reading a
			// closed connection as a finished run.
			writeSSEEvent(w, streamEventDraining, "")
			if flusher != nil {
				flusher.Flush()
			}
			return
		case msg, ok := <-events:
			if !ok {
				return
			}
			if msg == wsconn.StreamEventDone {
				writeSSE(w, "done")
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			writeSSE(w, msg)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// streamEventDraining names the SSE event this server sends before it stops.
// It is a named event rather than a reserved data payload because the data
// frames carry agent output, which can say anything.
const streamEventDraining = "draining"

// writeSSEComment writes an SSE comment frame. It carries no data, so the client
// ignores it; its only job is to move bytes so a proxy does not close an idle
// stream that the client would then read as a finished run.
func writeSSEComment(w http.ResponseWriter) {
	_, _ = w.Write([]byte(": ping\n\n"))
}

// writeSSEEvent writes a named event. An empty payload still gets a data line,
// because an event with no data is not delivered by every SSE parser.
func writeSSEEvent(w http.ResponseWriter, event, payload string) {
	_, _ = w.Write([]byte("event: " + event + "\n"))
	writeSSE(w, payload)
}

func writeSSE(w http.ResponseWriter, payload string) {
	if payload == "" {
		_, _ = w.Write([]byte("data: \n\n"))
		return
	}
	scanner := bufio.NewScanner(strings.NewReader(payload))
	for scanner.Scan() {
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(scanner.Bytes())
		_, _ = w.Write([]byte("\n"))
	}
	_, _ = w.Write([]byte("\n"))
}
