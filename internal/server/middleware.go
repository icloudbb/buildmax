package server

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	buildmaxlog "github.com/icloudbb/buildmax/internal/infra/log"
)

// RequestIDHeader returns the request's correlation id to the caller, so a bug
// report can name the request instead of describing it.
const RequestIDHeader = "X-Request-Id"

// corsMiddleware wraps h and adds CORS headers. allowedOrigin is the value for Access-Control-Allow-Origin (e.g. "http://localhost:5173").
// For OPTIONS (preflight), it responds with 204 and CORS headers without calling h.
//
// Allow-Credentials is set because the Portal sends its session cookie with
// `credentials: "include"`: a credentialed cross-origin request is blocked by
// the browser unless the response both names a specific origin (never "*", which
// allowedOrigin is) and allows credentials. Without it, a split-origin
// deployment — the Portal and server on different hosts or ports, as the Compose
// bundle runs them — can complete a preflight but never send the login POST.
func corsMiddleware(h http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// requestLoggingMiddleware gives each request an id, puts it in the context so
// every record logged under it carries the id, and logs one line when the
// request finishes.
//
// The line is written on the way out rather than on the way in because the
// facts worth having -- status, duration -- do not exist yet on the way in. An
// entry line would also double the volume to say something the exit line
// already implies.
func requestLoggingMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		ctx := buildmaxlog.With(r.Context(), "request_id", id)
		w.Header().Set(RequestIDHeader, id)

		rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		h.ServeHTTP(rec, r.WithContext(ctx))

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"bytes", rec.written,
			"remote", r.RemoteAddr,
		}
		if r.URL.RawQuery != "" {
			attrs = append(attrs, "query", redactQuery(r.URL.RawQuery))
		}
		slog.LogAttrs(ctx, statusLevel(rec.status), "request", slogArgs(attrs)...)
	})
}

// sensitiveQueryKeys are the substrings that mark a query parameter as carrying
// a credential. The WebSocket upgrade puts a JWT in ?token= because a browser
// cannot set a header on the upgrade (internal/server/handlers/ws.go), and
// recovery flows carry login codes; logging the raw query would copy either into
// every request line and any CI artifact that captures the logs.
var sensitiveQueryKeys = []string{"token", "code", "secret", "password", "passwd", "pwd", "jwt", "key", "auth", "sig", "otp", "credential"}

func isSensitiveQueryKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveQueryKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// redactQuery keeps the query's diagnostic value -- limit, offset, format -- while
// replacing the value of any credential-bearing key. A query that will not parse
// is dropped whole rather than logged raw: an unparseable string is exactly
// where a stray credential would hide.
func redactQuery(raw string) string {
	values, err := url.ParseQuery(raw)
	if err != nil {
		return "REDACTED"
	}
	for key, vs := range values {
		if isSensitiveQueryKey(key) {
			for i := range vs {
				vs[i] = "REDACTED"
			}
		}
	}
	return values.Encode()
}

// statusLevel keeps a level threshold meaningful: a refused request is the
// caller's problem, a 5xx is ours, and everything else is traffic.
func statusLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

func slogArgs(args []any) []slog.Attr {
	var rec slog.Record
	rec.Add(args...)
	out := make([]slog.Attr, 0, rec.NumAttrs())
	rec.Attrs(func(a slog.Attr) bool {
		out = append(out, a)
		return true
	})
	return out
}

// newRequestID is local rather than util.NewPrefixedID because a request is not
// an entity: it is never persisted and never named by another record, so it has
// no place in the prefix table in docs/contribute/conventions.md.
func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "rq_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "rq_" + hex.EncodeToString(b[:])
}

// responseRecorder records what the handler answered.
//
// Flush and Hijack are forwarded explicitly, not left to Unwrap: the SSE
// handlers assert http.Flusher directly and call the result without a nil
// check, so a wrapper that does not implement it turns every streamed response
// into a panic.
type responseRecorder struct {
	http.ResponseWriter
	status  int
	written int
	wrote   bool
}

func (rr *responseRecorder) WriteHeader(status int) {
	if !rr.wrote {
		rr.status = status
		rr.wrote = true
	}
	rr.ResponseWriter.WriteHeader(status)
}

func (rr *responseRecorder) Write(b []byte) (int, error) {
	rr.wrote = true
	n, err := rr.ResponseWriter.Write(b)
	rr.written += n
	return n, err
}

func (rr *responseRecorder) Flush() {
	if f, ok := rr.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (rr *responseRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := rr.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hj.Hijack()
}

// Unwrap lets http.ResponseController reach the real writer for everything not
// forwarded above.
func (rr *responseRecorder) Unwrap() http.ResponseWriter { return rr.ResponseWriter }
