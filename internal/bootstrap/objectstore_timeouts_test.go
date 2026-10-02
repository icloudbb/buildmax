package bootstrap

import "testing"

// The SDK client has no connect timeout of its own; without these a store that
// drops packets held a download for minutes before failing.
func TestStorageHTTPClientBoundsTheWaits(t *testing.T) {
	c := storageHTTPClient()
	if got := c.GetDialer().Timeout; got != storageDialTimeout {
		t.Errorf("dial timeout = %v, want %v", got, storageDialTimeout)
	}
	tr := c.GetTransport()
	if tr.TLSHandshakeTimeout != storageTLSHandshakeTimeout || tr.ResponseHeaderTimeout != storageResponseHeaderTimeout {
		t.Errorf("TLS handshake/response header = %v/%v, want %v/%v", tr.TLSHandshakeTimeout, tr.ResponseHeaderTimeout,
			storageTLSHandshakeTimeout, storageResponseHeaderTimeout)
	}
	if c.GetTimeout() != 0 {
		t.Errorf("overall client timeout = %v; it must stay 0 so a large transfer is not cut short", c.GetTimeout())
	}
}
