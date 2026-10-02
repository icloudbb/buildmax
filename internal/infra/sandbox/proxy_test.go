package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// TestProxy_PlainHTTP_Allow asserts an absolute-URL HTTP request to an
// allow-listed host succeeds through the proxy.
func TestProxy_PlainHTTP_Allow(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "hello %s", r.Host)
	}))
	defer origin.Close()
	originURL, _ := url.Parse(origin.URL)

	p, err := NewProxy(NewHostMatcher([]string{originURL.Hostname()}, nil), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer p.Close()

	client := proxyClient(t, p.Addr())
	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Errorf("status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	if !strings.HasPrefix(string(body), "hello ") {
		t.Errorf("body = %q", body)
	}
	if got := p.AllowCount(); got == 0 {
		t.Error("AllowCount = 0 after a successful request")
	}
}

// TestProxy_PlainHTTP_Deny asserts an absolute-URL request to a host
// outside the allow-list returns 403 with an LLM-readable reason.
func TestProxy_PlainHTTP_Deny(t *testing.T) {
	p, err := NewProxy(NewHostMatcher([]string{"only.allowed"}, nil), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer p.Close()

	client := proxyClient(t, p.Addr())
	resp, err := client.Get("http://blocked.example/")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("status = %d, want 403; body=%s", resp.StatusCode, body)
	}
	if !strings.Contains(string(body), "blocked.example") {
		t.Errorf("body should name the host: %s", body)
	}
	if got := p.DenyCount(); got == 0 {
		t.Error("DenyCount = 0 after a denied request")
	}
}

// TestProxy_Connect_Allow asserts CONNECT to an allow-listed host opens
// a tunnel.
func TestProxy_Connect_Allow(t *testing.T) {
	// Start a TLS-free TCP echo server so we can validate the tunnel
	// without bringing in real TLS.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Echo the first 5 bytes back.
		buf := make([]byte, 5)
		_, _ = io.ReadFull(conn, buf)
		_, _ = conn.Write(buf)
	}()

	host, _, _ := net.SplitHostPort(ln.Addr().String())
	p, err := NewProxy(NewHostMatcher([]string{host}, nil), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer p.Close()

	// Manual CONNECT
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", ln.Addr().String(), ln.Addr().String())
	respLine, _ := readLine(conn)
	if !strings.HasPrefix(respLine, "HTTP/1.1 200") {
		t.Fatalf("CONNECT response = %q, want 200", respLine)
	}
	// Drain header block ending in blank line.
	for {
		line, _ := readLine(conn)
		if line == "" {
			break
		}
	}
	// Tunnel established — send 5 bytes, expect echo.
	_, _ = conn.Write([]byte("hello"))
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read echoed bytes: %v", err)
	}
	if string(buf) != "hello" {
		t.Errorf("echo = %q, want hello", buf)
	}
}

// TestProxy_Connect_Deny asserts CONNECT to a non-allow-listed host gets
// 403 (and the proxy keeps the deny count).
func TestProxy_Connect_Deny(t *testing.T) {
	p, err := NewProxy(NewHostMatcher([]string{"only.allowed:443"}, nil), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer p.Close()
	conn, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "CONNECT denied.example:443 HTTP/1.1\r\nHost: denied.example:443\r\n\r\n")
	respLine, _ := readLine(conn)
	if !strings.Contains(respLine, "403") {
		t.Errorf("CONNECT to denied host got %q, want 403", respLine)
	}
	if p.DenyCount() == 0 {
		t.Error("DenyCount = 0")
	}
}

// TestProxy_SetMatcher asserts allow-list updates take effect on the
// next request without restarting the proxy.
func TestProxy_SetMatcher(t *testing.T) {
	p, err := NewProxy(NewHostMatcher(nil, nil), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	defer p.Close()
	client := proxyClient(t, p.Addr())
	resp, _ := client.Get("http://api.example/")
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("initial empty allow-list: status = %d, want 403", resp.StatusCode)
	}
	p.SetMatcher(NewHostMatcher([]string{"*"}, nil))
	// We still expect failure because the upstream doesn't exist, but
	// the failure mode should be 502 (bad gateway) instead of 403.
	resp, _ = client.Get("http://api.example/")
	if resp != nil {
		resp.Body.Close()
		if resp.StatusCode == 403 {
			t.Errorf("after SetMatcher(*), still 403")
		}
	}
}

// proxyClient builds an HTTP client that routes through the given proxy
// address. Used by the plain-HTTP tests.
func proxyClient(t *testing.T, addr string) *http.Client {
	t.Helper()
	u, err := url.Parse("http://" + addr)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
	}
}

// readLine reads bytes from r until "\r\n", returning the line without
// the trailing CRLF. Used by the CONNECT tests.
func readLine(r net.Conn) (string, error) {
	var buf strings.Builder
	one := make([]byte, 1)
	for {
		if _, err := io.ReadFull(r, one); err != nil {
			return buf.String(), err
		}
		if one[0] == '\n' {
			s := buf.String()
			return strings.TrimSuffix(s, "\r"), nil
		}
		buf.WriteByte(one[0])
	}
}

// A sandbox in its own network namespace reaches the proxy only through this
// socket, so it must serve the same filtered proxy as the TCP listener.
func TestProxy_UnixSocketServesTheSameFilter(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("origin-ok"))
	}))
	defer origin.Close()
	originURL, _ := url.Parse(origin.URL)
	p, err := NewProxy(NewHostMatcher([]string{originURL.Hostname()}, nil), nil)
	if err != nil {
		t.Fatalf("NewProxy: %v", err)
	}
	sock := p.SocketPath()
	if sock == "" {
		t.Fatal("proxy has no Unix socket")
	}
	viaSocket := &http.Client{Transport: &http.Transport{
		Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: "proxy.invalid"}),
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := viaSocket.Get(origin.URL)
	if err != nil {
		t.Fatalf("GET through the socket: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "origin-ok" {
		t.Fatalf("body = %q, want the origin's reply", body)
	}
	denied, err := viaSocket.Get("http://not-allowed.invalid/")
	if err == nil {
		_ = denied.Body.Close()
		if denied.StatusCode != http.StatusForbidden {
			t.Fatalf("a host off the allow-list answered %d through the socket, want 403", denied.StatusCode)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket %s left behind after Close: %v", sock, err)
	}
}
