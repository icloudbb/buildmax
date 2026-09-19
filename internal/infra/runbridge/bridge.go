// Package runbridge runs a per-run Unix-domain socket that a subprocess inside a
// worker run — the buildmax CLI launched through the Bash tool — uses to reach
// the worker API without ever holding the run token.
//
// The bridge is a reverse proxy to the internal worker listener. It injects the
// run token as a Bearer credential and forwards, so the token stays in the
// worker process: the subprocess is handed only the socket path. It forwards
// only worker routes; the run token is scoped to the run named in the path, so
// the server rejects any other run regardless. See
// docs/design/agent-bridge-cli.md.
package runbridge

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Bridge is a running socket server. Close it when the run ends.
type Bridge struct {
	server *http.Server
	socket string
	dir    string
}

// workerRoutePrefix is the only path space the bridge forwards. The run token it
// injects works nowhere else, so this is defense in depth, not the boundary.
const workerRoutePrefix = "/api/worker/"

// Serve starts the bridge and returns it once the socket is bound. serverURL is
// the worker listener origin, token is the run token to inject, and upstream
// supplies the transport used to reach the listener (its TLS/mTLS settings).
//
// The socket lives under a fresh 0700 temp directory rather than the run dirs,
// so its path stays short enough for the operating system's socket-name limit
// and never lands in an uploaded workspace or checkpoint.
func Serve(serverURL, token string, upstream *http.Client) (*Bridge, error) {
	target, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("parse worker server url: %w", err)
	}
	if target.Host == "" {
		return nil, fmt.Errorf("worker server url %q has no host", serverURL)
	}
	dir, err := os.MkdirTemp("", "buildmax-bridge-")
	if err != nil {
		return nil, fmt.Errorf("create bridge dir: %w", err)
	}
	// A short filename: a Unix socket path is bounded (sun_path, ~104 bytes), and
	// the temp dir already eats much of that budget.
	socket := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("listen on bridge socket: %w", err)
	}

	transport := http.DefaultTransport
	if upstream != nil && upstream.Transport != nil {
		transport = upstream.Transport
	}
	proxy := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			r.Host = target.Host
			// The subprocess never supplies a credential; the bridge is the only
			// place the run token is attached.
			r.Header.Set("Authorization", "Bearer "+token)
		},
		Transport: transport,
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, workerRoutePrefix) {
			http.Error(w, "the run bridge forwards only worker routes", http.StatusForbidden)
			return
		}
		proxy.ServeHTTP(w, r)
	})

	b := &Bridge{
		server: &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second},
		socket: socket,
		dir:    dir,
	}
	go func() { _ = b.server.Serve(ln) }()
	return b, nil
}

// SocketPath is the path to export as BUILDMAX_BRIDGE_SOCK.
func (b *Bridge) SocketPath() string {
	if b == nil {
		return ""
	}
	return b.socket
}

// Close stops the server and removes the socket directory. It is nil-safe.
func (b *Bridge) Close() error {
	if b == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = b.server.Shutdown(ctx)
	return os.RemoveAll(b.dir)
}
