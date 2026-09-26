package desktop

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

// TerminalOpen starts an interactive shell strand at the given project's
// workspace root and returns its strand id. Output arrives on eventTerminalData
// and the eventual exit on eventTerminalExit, both keyed by the returned id.
func (a *App) TerminalOpen(projectID string) (string, error) {
	proj, err := projectManager().Store().Get(context.Background(), projectID)
	if err != nil {
		return "", err
	}
	return a.terminals.open(proj.DefaultWorkspace)
}

// TerminalWrite forwards user keystrokes to a shell strand.
func (a *App) TerminalWrite(id, data string) error {
	return a.terminals.write(id, data)
}

// TerminalResize sets a shell strand's PTY window size to the pane geometry.
func (a *App) TerminalResize(id string, cols, rows int) error {
	return a.terminals.resize(id, cols, rows)
}

// TerminalClose terminates a shell strand and reaps its process.
func (a *App) TerminalClose(id string) error {
	return a.terminals.closeSession(id)
}

// Terminal event names. Each payload is keyed by strand id so the frontend can
// route bytes and exit to the right tab.
const (
	eventTerminalData = "desktop/terminal/data"
	eventTerminalExit = "desktop/terminal/exit"
)

// terminalReadChunk bounds one output event so a runaway process floods neither
// the Wails bridge nor the emulator in a single message.
const terminalReadChunk = 32 * 1024

// TerminalDataPayload carries one chunk of a shell's output. Chunk is
// base64-encoded raw PTY bytes: terminal output is not guaranteed valid UTF-8,
// and base64 preserves control sequences and binary output across JSON.
type TerminalDataPayload struct {
	ID    string `json:"id"`
	Chunk string `json:"chunk"`
}

// TerminalExitPayload reports that a shell strand's process ended.
type TerminalExitPayload struct {
	ID   string `json:"id"`
	Code int    `json:"code"`
}

// terminalSession is one interactive shell strand backed by a PTY.
type terminalSession struct {
	id   string
	ptmx *os.File
	cmd  *exec.Cmd
	// done closes once the pump has reaped this session, so a caller that killed
	// the shell can wait for cleanup to finish.
	done chan struct{}
}

// terminalManager owns the desktop's interactive shell strands: it spawns a PTY
// per strand, pumps output to the frontend, and reaps every process it owns on
// close and on app shutdown. It is deliberately Wails-agnostic — output and exit
// travel through the injected emit callback — so it is testable without a
// frontend. pty.Start reaps through the pump, which is the single owner of
// cmd.Wait for a session.
type terminalManager struct {
	mu       sync.Mutex
	sessions map[string]*terminalSession
	emit     func(name string, data any)
	newID    func() string
	// shellCommand builds the shell to run for a strand rooted at cwd. It is a
	// field so tests can substitute a deterministic program.
	shellCommand func(cwd string) *exec.Cmd
}

func newTerminalManager(emit func(name string, data any)) *terminalManager {
	var (
		cmu     sync.Mutex
		counter int
	)
	return &terminalManager{
		sessions: make(map[string]*terminalSession),
		emit:     emit,
		newID: func() string {
			cmu.Lock()
			defer cmu.Unlock()
			counter++
			return fmt.Sprintf("term-%d", counter)
		},
		shellCommand: defaultShellCommand,
	}
}

// defaultShellCommand runs the user's own interactive shell at cwd. The terminal
// is the user's authority, so it inherits the user's environment unscrubbed —
// unlike an Agent child (see docs/design/surface-positioning.md §5.4).
func defaultShellCommand(cwd string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell, "-i")
	cmd.Dir = cwd
	cmd.Env = os.Environ()
	return cmd
}

// open starts a shell strand at cwd and returns its id. Output pumps
// asynchronously; the strand lives until its process exits or close is called.
func (m *terminalManager) open(cwd string) (string, error) {
	cmd := m.shellCommand(cwd)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", fmt.Errorf("start terminal shell: %w", err)
	}
	id := m.newID()
	sess := &terminalSession{id: id, ptmx: ptmx, cmd: cmd, done: make(chan struct{})}
	m.mu.Lock()
	m.sessions[id] = sess
	m.mu.Unlock()
	go m.pump(sess)
	return id, nil
}

// pump forwards PTY output until the shell ends, then reaps the process and
// reports the exit. It is the only place cmd.Wait is called for a session, so
// close can kill the shell and let the pump own cleanup without racing Wait.
func (m *terminalManager) pump(sess *terminalSession) {
	buf := make([]byte, terminalReadChunk)
	for {
		n, err := sess.ptmx.Read(buf)
		if n > 0 {
			m.emit(eventTerminalData, TerminalDataPayload{
				ID:    sess.id,
				Chunk: base64.StdEncoding.EncodeToString(buf[:n]),
			})
		}
		if err != nil {
			break
		}
	}
	code := 0
	if werr := sess.cmd.Wait(); werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	m.mu.Lock()
	delete(m.sessions, sess.id)
	m.mu.Unlock()
	_ = sess.ptmx.Close()
	close(sess.done)
	m.emit(eventTerminalExit, TerminalExitPayload{ID: sess.id, Code: code})
}

// write sends user keystrokes to a shell strand.
func (m *terminalManager) write(id, data string) error {
	sess, err := m.session(id)
	if err != nil {
		return err
	}
	_, err = sess.ptmx.Write([]byte(data))
	return err
}

// resize sets the PTY window size so full-screen programs render at the pane's
// geometry. Non-positive dimensions are ignored to avoid a zero-size window.
func (m *terminalManager) resize(id string, cols, rows int) error {
	sess, err := m.session(id)
	if err != nil {
		return err
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	return pty.Setsize(sess.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// closeSession terminates a shell strand and waits for its pump to reap. Killing
// the shell makes the pump's Read return, so the pump — not this call — reaps
// and reports the exit.
func (m *terminalManager) closeSession(id string) error {
	sess, err := m.session(id)
	if err != nil {
		return err
	}
	if sess.cmd.Process != nil {
		_ = sess.cmd.Process.Kill()
	}
	_ = sess.ptmx.Close()
	<-sess.done
	return nil
}

// closeAll reaps every open strand. It is called on app shutdown so no shell is
// orphaned past the Desktop process.
func (m *terminalManager) closeAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.closeSession(id)
	}
}

func (m *terminalManager) session(id string) (*terminalSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[id]
	if !ok {
		return nil, fmt.Errorf("terminal %q not found", id)
	}
	return sess, nil
}
