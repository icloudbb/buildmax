//go:build linux

package main

import "golang.org/x/sys/unix"

// hideFromSandbox marks the worker non-dumpable. The sandbox re-binds the
// container's /proc, and the worker's environment holds the storage key and
// the run token; a dumpable process's /proc/<pid>/environ is readable by any
// process of the same uid, which Bash in the sandbox is. Non-dumpable, those
// files need CAP_SYS_PTRACE, which the worker pod never has and the sandbox
// drops. The flag resets on exec, so bwrap and Bash are unaffected.
func hideFromSandbox() error {
	return unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0)
}
