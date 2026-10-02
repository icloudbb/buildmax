package bootstrap

import (
	"net"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// A pooled connection whose peer vanished must be closed by the kernel, not
// waited on until the response-header timeout of every retry.
func TestStorageConnectionsBoundUnackedData(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	conn, err := storageHTTPClient().GetDialer().Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	raw, err := conn.(syscall.Conn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var got int
	var getErr error
	if err := raw.Control(func(fd uintptr) {
		got, getErr = unix.GetsockoptInt(int(fd), unix.IPPROTO_TCP, unix.TCP_USER_TIMEOUT)
	}); err != nil || getErr != nil {
		t.Fatalf("read TCP_USER_TIMEOUT: %v %v", err, getErr)
	}
	if want := int(storageUnackedTimeout.Milliseconds()); got != want {
		t.Errorf("TCP_USER_TIMEOUT = %dms, want %dms", got, want)
	}
}
