package desktop

import (
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// shSession makes the manager run a plain /bin/sh so the test does not depend on
// the developer's own $SHELL or its startup files. The PTY line discipline still
// echoes input, so writing a command yields both the echo and its output.
func shSession(m *terminalManager) {
	m.shellCommand = func(cwd string) *exec.Cmd {
		cmd := exec.Command("/bin/sh")
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "PS1=", "ENV=")
		return cmd
	}
}

func TestTerminalManagerRunsShellAndReports(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTY shell strands are not supported on Windows in this prototype")
	}
	var (
		mu     sync.Mutex
		output []byte
	)
	exited := make(chan int, 1)
	m := newTerminalManager(func(name string, data any) {
		switch p := data.(type) {
		case TerminalDataPayload:
			b, err := base64.StdEncoding.DecodeString(p.Chunk)
			if err != nil {
				t.Errorf("output chunk is not valid base64: %v", err)
				return
			}
			mu.Lock()
			output = append(output, b...)
			mu.Unlock()
		case TerminalExitPayload:
			select {
			case exited <- p.Code:
			default:
			}
		}
	})
	shSession(m)

	id, err := m.open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.write(id, "echo buildmax_terminal_ok\n"); err != nil {
		t.Fatalf("write: %v", err)
	}

	waitFor(t, 5*time.Second, "command output", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(string(output), "buildmax_terminal_ok")
	})

	if err := m.write(id, "exit\n"); err != nil {
		t.Fatalf("write exit: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("shell did not report exit")
	}

	m.mu.Lock()
	n := len(m.sessions)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected the exited strand to be reaped, still have %d", n)
	}
}

func TestTerminalManagerCloseReaps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTY shell strands are not supported on Windows in this prototype")
	}
	m := newTerminalManager(func(string, any) {})
	shSession(m)

	id, err := m.open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := m.resize(id, 100, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := m.closeSession(id); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := m.write(id, "x"); err == nil {
		t.Fatal("expected writing to a closed strand to fail")
	}

	m.mu.Lock()
	n := len(m.sessions)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("expected 0 strands after close, have %d", n)
	}
}

func TestTerminalManagerParallelStrandsAreIsolated(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("interactive PTY shell strands are not supported on Windows in this prototype")
	}
	m := newTerminalManager(func(string, any) {})
	shSession(m)

	a, err := m.open(t.TempDir())
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	b, err := m.open(t.TempDir())
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	if a == b {
		t.Fatalf("parallel strands share an id: %q", a)
	}

	m.mu.Lock()
	n := len(m.sessions)
	m.mu.Unlock()
	if n != 2 {
		t.Fatalf("expected 2 parallel strands, have %d", n)
	}

	// Closing one leaves the other running.
	if err := m.closeSession(a); err != nil {
		t.Fatalf("close a: %v", err)
	}
	if err := m.write(b, "echo still_here\n"); err != nil {
		t.Fatalf("second strand should still accept input: %v", err)
	}

	m.closeAll()
	m.mu.Lock()
	n = len(m.sessions)
	m.mu.Unlock()
	if n != 0 {
		t.Fatalf("closeAll should reap every strand, have %d", n)
	}
}

func waitFor(t *testing.T, d time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.After(d)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(20 * time.Millisecond):
		}
	}
}
