package mcp

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"

	mcpcfg "github.com/icloudbb/buildmax/internal/core/mcp"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func newTransport(cfg mcpcfg.ServerConfig, httpClient *http.Client) (mcpsdk.Transport, error) {
	if cfg.BearerTokenEnv != "" {
		if !safeBearerEndpoint(cfg.URL) {
			return nil, fmt.Errorf("MCP bearer token requires HTTPS or a loopback endpoint")
		}
		token := os.Getenv(cfg.BearerTokenEnv)
		if token == "" {
			return nil, fmt.Errorf("set %s to the MCP server bearer token", cfg.BearerTokenEnv)
		}
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		copyClient := *httpClient
		base := copyClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		copyClient.Transport = bearerTransport{base: base, token: token}
		copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		httpClient = &copyClient
	}
	switch cfg.Type {
	case "stdio":
		cmd := exec.Command(cfg.Command, cfg.Args...)
		if len(cfg.Env) > 0 {
			cmd.Env = mergedEnv(cfg.Env)
		}
		return &mcpsdk.CommandTransport{Command: cmd}, nil
	case "sse":
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		return &mcpsdk.SSEClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient}, nil
	case "http":
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		return &mcpsdk.StreamableClientTransport{Endpoint: cfg.URL, HTTPClient: httpClient}, nil
	default:
		return nil, fmt.Errorf("unknown mcp transport type %q", cfg.Type)
	}
}

func safeBearerEndpoint(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())
}

type bearerTransport struct {
	base  http.RoundTripper
	token string
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copyReq := req.Clone(req.Context())
	copyReq.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(copyReq)
}

// mergedEnv returns os.Environ() with keys in overrides replaced or appended.
func mergedEnv(overrides map[string]string) []string {
	if len(overrides) == 0 {
		return nil
	}
	return mergeEnv(os.Environ(), overrides)
}

// mergeEnv is the half that does not read the process environment, so the
// entries a platform produces can be given to it directly.
func mergeEnv(base []string, overrides map[string]string) []string {
	omit := make(map[string]struct{}, len(overrides))
	for k := range overrides {
		omit[k] = struct{}{}
	}
	out := make([]string, 0, len(base)+len(overrides))
	for _, e := range base {
		i := strings.IndexByte(e, '=')
		switch {
		case i < 0:
			// Not an environment entry at all; a child process would ignore it.
			continue
		case i == 0:
			// Windows records each drive's working directory as "=C:=C:\dir",
			// and cmd.exe adds "=ExitCode=". The name is empty, so neither can
			// match an override, and dropping them changes where a relative path
			// resolves for the MCP server this environment is built for.
			out = append(out, e)
			continue
		}
		if _, skip := omit[e[:i]]; skip {
			continue
		}
		out = append(out, e)
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}
