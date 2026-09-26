// Package mockllm serves scripted model replies over the three wire protocols
// BuildMax speaks, so an end-to-end suite can drive a real run without a
// provider, a key, or a paid call.
//
// A scenario is a list of steps, replayed in order: one step answers one model
// call, whatever protocol asked. Nothing is inferred from the request, because
// a mock that answers what it thinks was asked stops being evidence — the run
// it produces is the mock's opinion rather than the agent's behaviour.
//
// How long a reply takes is the exception, and it is set out of band rather
// than guessed: a step can script its own stall, and a suite driving a deployed
// mock arms one over the control route. A test that has to act on a run while
// the run is still going — cancel it, take its worker away — otherwise races
// the run to its own end, and duration is the one property of a scripted turn
// that says nothing about what the agent did.
//
// See docs/design/end-to-end-testing.md §4.
package mockllm

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ToolCall is one call the scripted assistant turn makes.
type ToolCall struct {
	// ID is what the tool result will reference. Left empty, the handler names
	// it after its position so a scenario does not have to invent ids.
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

// Usage is the token report for one step. Runs assert on it, so it is scripted
// rather than invented per request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Step is one scripted model reply.
//
// A step carries several tool calls because the runtime schedules a turn's
// calls concurrently: a format with one call per turn could not express the
// case that scheduling has to leave unchanged. See
// docs/design/parallel-tool-execution.md.
type Step struct {
	Text      string     `json:"text,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Usage     *Usage     `json:"usage,omitempty"`
	// Status and Error make the step a provider failure instead of a reply.
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	// DelayMS holds the reply back before writing it, so a run stays mid-turn
	// long enough for a suite to act on it. A failure step delays too: a
	// provider that fails slowly is what a timeout looks like from here.
	DelayMS int `json:"delay_ms,omitempty"`
}

// Scenario is the ordered script for one run.
type Scenario struct {
	Name  string `json:"name,omitempty"`
	Steps []Step `json:"steps"`
	// Repeat replays the last step for every call past the end instead of
	// failing. It exists for the deployment smoke, which asserts on outcomes
	// rather than on how many turns reaching them took, and must stay opt-in:
	// everywhere else, a call past the end of the script is the finding.
	Repeat bool `json:"repeat,omitempty"`
}

// LoadScenario reads a committed scenario file.
func LoadScenario(path string) (Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario: %w", err)
	}
	var s Scenario
	if err := json.Unmarshal(data, &s); err != nil {
		return Scenario{}, fmt.Errorf("parse scenario %s: %w", path, err)
	}
	if len(s.Steps) == 0 {
		return Scenario{}, fmt.Errorf("scenario %s has no steps", path)
	}
	return s, nil
}

// Protocol names the wire protocol a request arrived on. The values match
// config.LLMProvider* so a suite can configure a model entry from one constant.
const (
	ProtocolOpenAIChat      = "openai_compatible"
	ProtocolOpenAIResponses = "openai"
	ProtocolAnthropic       = "anthropic"
)

// Request is one model call the agent made, kept so a suite can assert on the
// history it sent as well as on what came back.
type Request struct {
	Protocol string
	Stream   bool
	Body     []byte
	// Credential is the key the caller presented, from whichever header its
	// protocol uses. It is what lets a suite prove which key a rotated model
	// credential actually sent upstream.
	Credential string
}

// Handler replays a scenario over HTTP.
type Handler struct {
	mu       sync.Mutex
	steps    []Step
	repeat   bool
	next     int
	requests []Request
	stall    time.Duration
	// armedSteps is a FIFO of one-shot overrides, the same reason Stall
	// exists: a deployed mock's script is mounted before anything runs, so a
	// suite that decides mid-run it needs specific calls to do something the
	// mounted scenario does not — call a tool the suite names, to prove a
	// real subprocess ran and was or was not confined — has no other way to
	// say so. A queue rather than a single override because the caller
	// dispatching a task is not always the very next call the runtime makes
	// of the model; arming N covers however many calls come before the one
	// the suite actually cares about. Popped front-first and never advances
	// h.next, so the first call past the queue resumes exactly where
	// Steps/Repeat would have answered had none of this happened.
	armedSteps []Step
}

// ControlStallPath is the route that arms a stall on a mock a suite cannot
// rescript — one already deployed, answering a stack the suite only reaches
// over HTTP. It is matched by suffix so an ingress may serve it under a prefix.
const ControlStallPath = "/control/stall"

// ControlToolCallPath arms a one-shot tool call the same way ControlStallPath
// arms a stall. See Handler.armedStep.
const ControlToolCallPath = "/control/toolcall"

// ControlRequestsPath returns every request the mock has received so far, so
// a suite driving a deployed mock over HTTP can inspect a tool result a
// scripted final reply never echoes back. GET, unlike every other route here.
const ControlRequestsPath = "/control/requests"

// NewHandler returns a handler that replays s.
func NewHandler(s Scenario) *Handler { return &Handler{steps: s.Steps, repeat: s.Repeat} }

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, ControlRequestsPath) {
		h.serveControlRequests(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "mockllm: only POST", http.StatusMethodNotAllowed)
		return
	}
	if strings.HasSuffix(r.URL.Path, ControlStallPath) {
		h.serveControlStall(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, ControlToolCallPath) {
		h.serveControlToolCall(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "mockllm: read request: "+err.Error(), http.StatusBadRequest)
		return
	}
	protocol, ok := protocolFor(r.URL.Path)
	if !ok {
		http.Error(w, fmt.Sprintf("mockllm: no protocol serves %s", r.URL.Path), http.StatusNotFound)
		return
	}
	stream := requestsStream(body)
	step, index, ok := h.take(Request{Protocol: protocol, Stream: stream, Body: body, Credential: credentialOf(r)})
	if !ok {
		// Repeating the last step here would turn "the run called the model
		// more times than the scenario describes" into a passing test. The
		// status is a client error so the caller's retry loop does not spend
		// its backoff on a fault no retry can fix.
		http.Error(w, "mockllm: scenario exhausted, the run made more model calls than it scripts", http.StatusBadRequest)
		return
	}
	if delay := h.delayFor(step); delay > 0 {
		select {
		case <-time.After(delay):
		case <-r.Context().Done():
			// The caller gave up, or the process is going down. Writing a reply
			// nobody is reading would only hold the shutdown open.
			return
		}
	}
	if step.Status != 0 {
		message := step.Error
		if message == "" {
			message = "scripted provider failure"
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(step.Status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": message, "type": "mockllm"}})
		return
	}
	step = named(step, index)
	switch protocol {
	case ProtocolOpenAIChat:
		writeOpenAIChat(w, step, modelOf(body), stream)
	case ProtocolOpenAIResponses:
		writeOpenAIResponses(w, step, modelOf(body), stream)
	case ProtocolAnthropic:
		writeAnthropic(w, step, modelOf(body), stream)
	}
}

// credentialOf reads the key a model call carried: a bearer token for the
// OpenAI protocols, x-api-key for Anthropic.
func credentialOf(r *http.Request) string {
	if key := r.Header.Get("x-api-key"); key != "" {
		return key
	}
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// serveControlStall arms or clears the stall every later reply waits out.
//
// It is deliberately not a scenario step: the deployed mock is built into an
// image and its script is mounted before anything runs, so a suite that decides
// mid-run that it needs a slow turn has no other way to say so.
func (h *Handler) serveControlStall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MS int `json:"ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "mockllm: parse stall request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.MS < 0 {
		http.Error(w, "mockllm: stall ms must not be negative", http.StatusBadRequest)
		return
	}
	h.Stall(time.Duration(body.MS) * time.Millisecond)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"stall_ms": body.MS})
}

// Stall makes every later reply wait d before it is written. Zero clears it.
// It applies to replies the mock has not started yet, never to one in flight.
func (h *Handler) Stall(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stall = d
}

// serveControlToolCall arms the one-shot tool call the next reply answers
// with, instead of whatever Steps/Repeat would otherwise have said.
func (h *Handler) serveControlToolCall(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name  string         `json:"name"`
		Args  map[string]any `json:"args,omitempty"`
		Times int            `json:"times,omitempty"`
		// Clear drops every still-queued arm without consuming a call, so a
		// suite that armed more than the run actually used cannot leak an
		// unconsumed override into whatever runs next.
		Clear bool `json:"clear,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "mockllm: parse tool call request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Clear {
		h.ClearArmedToolCalls()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"cleared": true})
		return
	}
	if body.Name == "" {
		http.Error(w, "mockllm: tool call name required", http.StatusBadRequest)
		return
	}
	times := body.Times
	if times <= 0 {
		times = 1
	}
	h.ArmToolCall(body.Name, body.Args, times)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"armed": body.Name, "times": times})
}

// ArmToolCall queues times consecutive replies that answer with exactly one
// call to name, regardless of what Steps/Repeat scripts there. A dispatched
// task is rarely the very next call the runtime makes of the model — a
// routing or classification call ahead of it is common — so times covers
// however many of those come first; each is popped and discarded the same as
// the one the suite actually wanted.
func (h *Handler) ArmToolCall(name string, args map[string]any, times int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for range times {
		h.armedSteps = append(h.armedSteps, Step{ToolCalls: []ToolCall{{Name: name, Args: args}}})
	}
}

// ClearArmedToolCalls drops every queued arm that was never consumed.
func (h *Handler) ClearArmedToolCalls() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.armedSteps = nil
}

// serveControlRequests answers every request the mock has recorded so far, so
// a suite driving a deployed mock over HTTP can inspect a tool result a
// scripted final reply never echoes back — the only way to see one, since the
// mock cannot be imported as a Go value from outside its own process.
func (h *Handler) serveControlRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "mockllm: only GET", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(h.Requests())
}

// delayFor is the longer of the step's own stall and the armed one, so arming
// one cannot make a step that scripts a longer delay finish early.
func (h *Handler) delayFor(step Step) time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	scripted := time.Duration(step.DelayMS) * time.Millisecond
	if h.stall > scripted {
		return h.stall
	}
	return scripted
}

// take consumes the next step and records the call that consumed it. An
// armed one-shot tool call answers before Steps/Repeat is even consulted, and
// does not advance h.next: the call after it resumes exactly where the
// script would have answered had the arming never happened.
func (h *Handler) take(req Request) (Step, int, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests = append(h.requests, req)
	if len(h.armedSteps) > 0 {
		step := h.armedSteps[0]
		h.armedSteps = h.armedSteps[1:]
		return step, h.next, true
	}
	if h.next >= len(h.steps) {
		if !h.repeat || len(h.steps) == 0 {
			return Step{}, 0, false
		}
		last := len(h.steps) - 1
		return h.steps[last], last, true
	}
	step := h.steps[h.next]
	index := h.next
	h.next++
	return step, index, true
}

// Remaining reports how many steps were never consumed. A suite fails on a
// non-zero value: a run that stopped calling the model one turn early leaves
// the final output looking plausible, and this is the only signal that says
// otherwise.
func (h *Handler) Remaining() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.steps) - h.next
}

// Requests returns the calls made so far, in order.
func (h *Handler) Requests() []Request {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Request(nil), h.requests...)
}

// Server is a handler listening on a local port.
type Server struct {
	*Handler
	listener net.Listener
	http     *http.Server
	done     chan struct{}
}

// Start serves scenario on a loopback port until Close.
func Start(scenario Scenario) (*Server, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("mockllm listen: %w", err)
	}
	handler := NewHandler(scenario)
	srv := &Server{Handler: handler, listener: listener, http: &http.Server{Handler: handler}, done: make(chan struct{})}
	go func() {
		defer close(srv.done)
		_ = srv.http.Serve(listener)
	}()
	return srv, nil
}

// Close stops serving.
func (s *Server) Close() {
	_ = s.http.Close()
	<-s.done
}

// URL is the server's root.
func (s *Server) URL() string { return "http://" + s.listener.Addr().String() }

// BaseURL is the api_url a model entry needs for this protocol. The two
// families disagree about where the version segment lives: the OpenAI client
// appends its path to the configured base, and the Anthropic client appends
// "v1/messages" of its own.
func (s *Server) BaseURL(protocol string) string {
	if protocol == ProtocolAnthropic {
		return s.URL()
	}
	return s.URL() + "/v1"
}

func protocolFor(path string) (string, bool) {
	switch {
	case strings.HasSuffix(path, "/chat/completions"):
		return ProtocolOpenAIChat, true
	case strings.HasSuffix(path, "/responses"):
		return ProtocolOpenAIResponses, true
	case strings.HasSuffix(path, "/messages"):
		return ProtocolAnthropic, true
	}
	return "", false
}

// named gives every tool call an id, derived from where it sits in the
// scenario so the same script always produces the same ids.
func named(step Step, index int) Step {
	calls := make([]ToolCall, len(step.ToolCalls))
	for i, call := range step.ToolCalls {
		if call.ID == "" {
			call.ID = fmt.Sprintf("call_%d_%d", index+1, i+1)
		}
		calls[i] = call
	}
	step.ToolCalls = calls
	return step
}

func requestsStream(body []byte) bool {
	var req struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(body, &req)
	return req.Stream
}

func modelOf(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &req) != nil || req.Model == "" {
		return "mockllm"
	}
	return req.Model
}

// argumentsJSON renders a call's arguments as the JSON string the OpenAI
// protocols carry. An unencodable value becomes an empty object rather than
// failing the request: the scenario is the thing under test, not the encoder.
func argumentsJSON(call ToolCall) string {
	if call.Args == nil {
		return "{}"
	}
	encoded, err := json.Marshal(call.Args)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}
