package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Proxy is the in-process HTTP forward proxy used by the bash sandbox.
// It supports HTTP CONNECT tunnels (HTTPS) and plain HTTP GETs.
// The proxy never inspects or terminates TLS — it filters by hostname
// only, matching Claude Code's documented behavior ("the built-in proxy
// enforces the allowlist based on the requested hostname and does not
// terminate or inspect TLS traffic").
//
// The proxy listens on 127.0.0.1 by default. Tests can pass any
// resolved address to NewProxy.
type Proxy struct {
	listener net.Listener
	server   *http.Server
	// socketPath is a Unix-domain socket serving the same proxy, for a sandbox
	// in its own network namespace: 127.0.0.1 there is not this process's, so
	// the sandbox bridges its loopback port to this file instead. Empty when
	// the socket could not be created, which leaves the sandbox on the shared
	// network.
	socketPath string

	matcherMu sync.RWMutex
	matcher   *HostMatcher

	violations Violator // optional; emits one event per allow/deny decision

	allowCount uint64
	denyCount  uint64
}

// Violator is the side-channel for proxy decisions. Phase E plugs it into
// the violation store; until then, Manager passes nil.
type Violator interface {
	OnAllow(host string)
	OnDeny(host, reason string)
}

// NewProxy starts a proxy listening on `127.0.0.1:0` (random port).
// Returns immediately once the listener is bound. The Manager owns the
// proxy lifecycle.
func NewProxy(matcher *HostMatcher, viol Violator) (*Proxy, error) {
	if matcher == nil {
		matcher = NewHostMatcher(nil, nil)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("sandbox proxy: listen: %w", err)
	}
	p := &Proxy{
		listener:   ln,
		matcher:    matcher,
		violations: viol,
	}
	p.server = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          nil, // suppress noisy default logging
	}
	go func() {
		if err := p.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			proxyLog().Warn("serve exited", "err", err)
		}
	}()
	if dir, err := os.MkdirTemp("", "buildmax-proxy-"); err != nil {
		proxyLog().Warn("unix socket unavailable; sandbox network stays shared", "err", err)
	} else if uln, err := net.Listen("unix", filepath.Join(dir, "proxy.sock")); err != nil {
		_ = os.RemoveAll(dir)
		proxyLog().Warn("unix socket unavailable; sandbox network stays shared", "err", err)
	} else {
		p.socketPath = filepath.Join(dir, "proxy.sock")
		go func() {
			if err := p.server.Serve(uln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				proxyLog().Warn("unix serve exited", "err", err)
			}
		}()
	}
	return p, nil
}

// SocketPath returns the Unix-domain socket serving this proxy, or "" when
// there is none.
func (p *Proxy) SocketPath() string {
	if p == nil {
		return ""
	}
	return p.socketPath
}

// Addr returns the proxy's bound address (e.g. "127.0.0.1:54321").
func (p *Proxy) Addr() string {
	if p == nil || p.listener == nil {
		return ""
	}
	return p.listener.Addr().String()
}

// SetMatcher swaps the host filter atomically. Phase E uses this for
// dynamic settings refresh.
func (p *Proxy) SetMatcher(m *HostMatcher) {
	if p == nil {
		return
	}
	if m == nil {
		m = NewHostMatcher(nil, nil)
	}
	p.matcherMu.Lock()
	p.matcher = m
	p.matcherMu.Unlock()
}

// AllowCount / DenyCount return cumulative counters since the proxy
// started. Used by `buildmax sandbox status` for at-a-glance visibility.
func (p *Proxy) AllowCount() uint64 {
	if p == nil {
		return 0
	}
	return atomic.LoadUint64(&p.allowCount)
}
func (p *Proxy) DenyCount() uint64 {
	if p == nil {
		return 0
	}
	return atomic.LoadUint64(&p.denyCount)
}

// Close stops the proxy. Safe to call on a nil proxy.
func (p *Proxy) Close() error {
	if p == nil || p.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.server.Shutdown(ctx)
	if p.socketPath != "" {
		_ = os.RemoveAll(filepath.Dir(p.socketPath))
	}
	return err
}

// ServeHTTP handles both CONNECT (HTTPS tunneling) and plain HTTP
// proxying. Other methods are rejected.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodConnect {
		p.handleConnect(w, r)
		return
	}
	if r.URL.IsAbs() {
		p.handlePlainHTTP(w, r)
		return
	}
	http.Error(w, "sandbox proxy: only HTTP CONNECT and absolute HTTP proxy requests are supported", http.StatusBadRequest)
}

// handleConnect implements the HTTPS tunnel after checking the host.
// On allow: respond 200 OK and bidirectional copy. On deny: 403 with
// the host name and a short reason so curl/wget surface a readable error.
func (p *Proxy) handleConnect(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Host
	if host == "" {
		host = r.Host
	}
	if ok, reason := p.allow(host); !ok {
		p.recordDeny(host, reason)
		http.Error(w, reason, http.StatusForbidden)
		return
	}
	p.recordAllow(host)

	upstream, err := net.DialTimeout("tcp", host, 15*time.Second)
	if err != nil {
		http.Error(w, "sandbox proxy: dial upstream: "+err.Error(), http.StatusBadGateway)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "sandbox proxy: hijack unsupported", http.StatusInternalServerError)
		return
	}
	client, _, err := hj.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	// We have already not written any body yet; we must write the 200
	// response on the raw conn ourselves.
	if _, err := client.Write([]byte("HTTP/1.1 200 OK\r\n\r\n")); err != nil {
		_ = upstream.Close()
		_ = client.Close()
		return
	}
	go pipe(upstream, client)
	pipe(client, upstream)
}

// handlePlainHTTP forwards an absolute-URL HTTP/1.1 request to upstream,
// after the host check.
func (p *Proxy) handlePlainHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Host
	if host == "" {
		host = r.Host
	}
	if ok, reason := p.allow(host); !ok {
		p.recordDeny(host, reason)
		http.Error(w, reason, http.StatusForbidden)
		return
	}
	p.recordAllow(host)

	out, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
	if err != nil {
		http.Error(w, "sandbox proxy: build upstream request: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Strip hop-by-hop headers per RFC 7230 §6.1.
	for k, vs := range r.Header {
		if isHopByHop(k) {
			continue
		}
		out.Header[k] = vs
	}
	resp, err := proxyHTTPClient.Do(out)
	if err != nil {
		http.Error(w, "sandbox proxy: upstream error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	for k, vs := range resp.Header {
		if isHopByHop(k) {
			continue
		}
		w.Header()[k] = vs
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// allow snapshots the current matcher and consults it.
func (p *Proxy) allow(host string) (bool, string) {
	p.matcherMu.RLock()
	m := p.matcher
	p.matcherMu.RUnlock()
	return m.Allowed(host)
}

func (p *Proxy) recordAllow(host string) {
	atomic.AddUint64(&p.allowCount, 1)
	if p.violations != nil {
		p.violations.OnAllow(host)
	}
}
func (p *Proxy) recordDeny(host, reason string) {
	atomic.AddUint64(&p.denyCount, 1)
	if p.violations != nil {
		p.violations.OnDeny(host, reason)
	}
}

// proxyHTTPClient is the upstream client used for plain-HTTP forwarding.
// Separate from the agent's other clients so it has its own connection
// pool and a conservative timeout.
var proxyHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		// Don't follow redirects: that's the client's job. Returning the
		// redirect itself preserves end-to-end semantics.
		return http.ErrUseLastResponse
	},
}

// pipe copies src→dst until either side closes, then signals completion
// by closing the writer side. Used to bridge CONNECT tunnels.
func pipe(dst io.WriteCloser, src io.ReadCloser) {
	_, _ = io.Copy(dst, src)
	_ = dst.Close()
	_ = src.Close()
}

// isHopByHop reports whether a header should be stripped per RFC 7230
// when forwarding through a proxy. Case-insensitive.
func isHopByHop(name string) bool {
	switch strings.ToLower(name) {
	case "connection",
		"proxy-connection",
		"keep-alive",
		"proxy-authenticate",
		"proxy-authorization",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade":
		return true
	}
	return false
}

// Identity belongs in an attr, not in every message string.
func proxyLog() *slog.Logger { return slog.With("component", "sandbox_proxy") }
