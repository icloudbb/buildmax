//go:build linux

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"testing"
)

// A Bash command in the sandbox runs as the worker's uid and sees the
// container's /proc. Once the worker is non-dumpable, its environment -- the
// storage key and run token -- is not readable that way.
func TestHideFromSandboxClosesProcEnviron(t *testing.T) {
	if os.Getenv("BUILDMAX_TEST_HIDE_CHILD") == "1" {
		if err := hideFromSandbox(); err != nil {
			fmt.Println("prctl failed:", err)
			os.Exit(1)
		}
		fmt.Println("ready")
		select {}
	}
	if os.Geteuid() == 0 {
		t.Skip("root holds CAP_SYS_PTRACE outside a container and reads any environ; the worker pod drops it")
	}
	child := exec.Command(os.Args[0], "-test.run=^TestHideFromSandboxClosesProcEnviron$")
	child.Env = append(os.Environ(), "BUILDMAX_TEST_HIDE_CHILD=1", "BUILDMAX_TEST_SECRET=should-not-be-readable")
	out, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "ready\n" {
		t.Fatalf("child did not get ready: %q", line)
	}
	_, err = os.ReadFile(fmt.Sprintf("/proc/%d/environ", child.Process.Pid))
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("reading a non-dumpable process's environ: err=%v, want permission denied", err)
	}
}
