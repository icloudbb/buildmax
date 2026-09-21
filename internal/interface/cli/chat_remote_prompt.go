package cli

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// remotePromptMsg is a follow-up prompt that arrived from another device through
// Remote Control. It is handled exactly like a locally typed message.
type remotePromptMsg struct{ text string }

// remotePromptSink bridges the Remote Control relay — a goroutine the AgentApp
// owns — to the Bubble Tea model. The relay calls Deliver from its own goroutine;
// the program is wired in after tea.NewProgram, the same lifecycle the approval
// handler uses (see chat_approval.go).
type remotePromptSink struct {
	mu      sync.Mutex
	program *tea.Program
}

func newRemotePromptSink() *remotePromptSink { return &remotePromptSink{} }

// SetProgram wires in the program once it exists, after tea.NewProgram.
func (s *remotePromptSink) SetProgram(p *tea.Program) {
	s.mu.Lock()
	s.program = p
	s.mu.Unlock()
}

// Deliver forwards a remote prompt into the model. It is safe to call before the
// program is wired in: a prompt that races startup is dropped, which is
// acceptable for a best-effort follow-up.
func (s *remotePromptSink) Deliver(content string) {
	s.mu.Lock()
	p := s.program
	s.mu.Unlock()
	if p != nil {
		p.Send(remotePromptMsg{text: content})
	}
}

// handleRemotePrompt treats a remote prompt exactly as a locally typed message:
// enqueued if a run is active — the running turn picks it up at its next
// iteration boundary and the injection is shown like any mid-run message — or
// started as a new turn if idle.
func handleRemotePrompt(m *Model, text string) (tea.Model, tea.Cmd) {
	text = strings.TrimSpace(text)
	if text == "" {
		return m, nil
	}
	if m.busy {
		if _, err := m.queue.Enqueue(text); err != nil {
			m.err = fmt.Sprintf("remote prompt dropped: %v", err)
		}
		return m, nil
	}
	return m, startRun(m, text)
}
