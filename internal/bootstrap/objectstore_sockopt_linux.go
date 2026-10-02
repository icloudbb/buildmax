package bootstrap

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// boundUnackedData sets TCP_USER_TIMEOUT on a storage connection, so the kernel
// closes it when sent data stays unacknowledged for storageUnackedTimeout.
func boundUnackedData(_, _ string, c syscall.RawConn) error {
	var setErr error
	if err := c.Control(func(fd uintptr) {
		setErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT, int(storageUnackedTimeout.Milliseconds()))
	}); err != nil {
		return err
	}
	return setErr
}
