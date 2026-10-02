//go:build !linux

package bootstrap

import "syscall"

// boundUnackedData is Linux-only: TCP_USER_TIMEOUT has no portable equivalent,
// and the servers that reach object storage run on Linux. Elsewhere a dead
// pooled connection is bounded by the response-header timeout alone.
func boundUnackedData(_, _ string, _ syscall.RawConn) error { return nil }
