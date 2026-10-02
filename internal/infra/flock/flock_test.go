package flock

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// helperReady is the line the helper prints once it holds the lock. It is a
// fixed token, not a word like "held", so the helper's own failure output can
// never be mistaken for the ready signal: ErrHeld's message contains "held".
const helperReady = "flock-helper-holds-the-lock"

func TestAcquireAndRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "held")
	l, err := TryAcquire(path, []byte("session-a"))
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	// Released even when an assertion below fails: on Windows a held file
	// cannot be deleted, so leaking the lock turns one failure into a
	// confusing cleanup error on top of it.
	t.Cleanup(func() { _ = l.Release() })

	// Readable while held. Windows locks are mandatory, so a lock over the
	// holder line would deny the one read it exists for.
	if got := string(Holder(path)); got != "session-a" {
		t.Fatalf("Holder = %q, want the holder line just written", got)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatalf("second Release: %v, want it to be safe", err)
	}
}

// TestHeldByAnotherProcess is the property the package exists for. The lock
// must be refused while another process holds it — testing it in-process would
// pass on some platforms for the wrong reason, since a second lock request
// from the same process can be granted.
func TestHeldByAnotherProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "held")

	helper := exec.Command(os.Args[0], "-test.run=TestHelperHoldsLock")
	helper.Env = append(os.Environ(), "FLOCK_HELPER_PATH="+path, "FLOCK_HELPER=1")
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = helper.Process.Kill()
		_ = helper.Wait()
	}()

	// The helper prints one line once it holds the lock. Read a whole line and
	// match it exactly: a partial read or the helper's own failure output must
	// not be taken for the ready signal, or the lock check below runs before the
	// lock is held and fails intermittently.
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != helperReady {
		t.Fatalf("helper did not report holding the lock: %q, %v", line, err)
	}

	// Capture the lock so it can be released: if it is wrongly granted, leaking
	// the handle would leave the file open and turn the assertion failure into a
	// confusing TempDir cleanup error on Windows, where a held file cannot be
	// deleted.
	if l, err := TryAcquire(path, []byte("session-b")); !errors.Is(err, ErrHeld) {
		_ = l.Release()
		t.Fatalf("TryAcquire while held = %v, want ErrHeld", err)
	}
	if got := string(Holder(path)); !strings.Contains(got, "helper") {
		t.Fatalf("Holder = %q, want the helper's line so a refusal can name it", got)
	}

	// The lock is the helper's to drop: killing it must free the lock with no
	// staleness rule involved.
	_ = helper.Process.Kill()
	_ = helper.Wait()

	l, err := TryAcquire(path, []byte("session-b"))
	if err != nil {
		t.Fatalf("TryAcquire after the holder died = %v, want the lock", err)
	}
	_ = l.Release()
}

// TestHelperHoldsLock is the child process for TestHeldByAnotherProcess. It is
// inert in a normal run.
func TestHelperHoldsLock(t *testing.T) {
	if os.Getenv("FLOCK_HELPER") != "1" {
		t.Skip("helper process only")
	}
	l, err := TryAcquire(os.Getenv("FLOCK_HELPER_PATH"), []byte("helper"))
	if err != nil {
		t.Fatalf("helper TryAcquire: %v", err)
	}
	defer func() { _ = l.Release() }()
	os.Stdout.WriteString(helperReady + "\n")
	// Sleep, not select{}: run without -test.timeout this process has no
	// timer, so select{} is a deadlock the runtime kills at once -- which
	// released the lock before the parent's check and failed it at random.
	time.Sleep(time.Hour) // killed by the parent
}
