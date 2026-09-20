package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coremcp "github.com/icloudbb/buildmax/internal/core/mcp"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func runMCPCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewRootCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestConnectMCPAndUseCLI(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvKeyBuildmaxHome, home)
	t.Setenv("TEST_MCP_BEARER", "fixture-secret")
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "fixture", Version: "1"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "echo", Description: "Echo a message", Annotations: &mcpsdk.ToolAnnotations{ReadOnlyHint: true},
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}},
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, in map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		return nil, map[string]any{"echo": in["message"]}, nil
	})
	mcpsdk.AddTool(server, &mcpsdk.Tool{
		Name: "mutate", Description: "Change something", InputSchema: map[string]any{"type": "object"},
	}, func(_ context.Context, _ *mcpsdk.CallToolRequest, _ map[string]any) (*mcpsdk.CallToolResult, map[string]any, error) {
		t.Error("unconfirmed write reached server")
		return nil, nil, nil
	})
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return server }, &mcpsdk.StreamableHTTPOptions{JSONResponse: true})
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()

	path := filepath.Join(home, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"custom":{"keep":true},"mcpServers":{"existing":{"type":"stdio","command":"echo"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := runMCPCLI(t, "connect", "mcp", "fixture", httpServer.URL, "--bearer-env", "TEST_MCP_BEARER")
	if err != nil || !strings.Contains(out, `Connected MCP server "fixture"`) {
		t.Fatalf("connect: %v, output %q", err, out)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Custom     json.RawMessage                 `json:"custom"`
		MCPServers map[string]coremcp.ServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Custom) == 0 || len(doc.MCPServers) != 2 || doc.MCPServers["fixture"].BearerTokenEnv != "TEST_MCP_BEARER" || bytes.Contains(raw, []byte("fixture-secret")) {
		t.Fatalf("incorrect saved config: %s", raw)
	}
	if out, err := runMCPCLI(t, "mcp", "list"); err != nil || !strings.Contains(out, "fixture\thttp") || strings.Contains(out, "fixture-secret") {
		t.Fatalf("list: %v, %q", err, out)
	}
	if out, err := runMCPCLI(t, "mcp", "tools", "fixture"); err != nil || !strings.Contains(out, "echo") {
		t.Fatalf("tools: %v, %q", err, out)
	}
	if out, err := runMCPCLI(t, "mcp", "schema", "fixture", "echo"); err != nil || !strings.Contains(out, "input_schema") {
		t.Fatalf("schema: %v, %q", err, out)
	}
	if out, err := runMCPCLI(t, "mcp", "call", "fixture", "echo", "--json", `{"message":"hello"}`); err != nil || !strings.Contains(out, "hello") {
		t.Fatalf("call: %v, %q", err, out)
	}
	if _, err := runMCPCLI(t, "mcp", "call", "fixture", "mutate"); err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("unconfirmed write: %v", err)
	}
	if _, err := runMCPCLI(t, "connect", "mcp", "fixture", httpServer.URL); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate connection: %v", err)
	}
	t.Setenv("TEST_MCP_BEARER", "")
	if _, err := runMCPCLI(t, "mcp", "tools", "fixture"); err == nil || !strings.Contains(err.Error(), "TEST_MCP_BEARER") {
		t.Fatalf("missing token did not fail closed: %v", err)
	}
}

func TestConnectMCPRejectsUnsafeOrUnreachableURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvKeyBuildmaxHome, home)
	for _, raw := range []string{"http://example.com/mcp", "https://user:secret@example.com/mcp", "https://example.com/mcp?token=secret"} {
		if _, err := runMCPCLI(t, "connect", "mcp", "fixture", raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if _, err := runMCPCLI(t, "connect", "mcp", "fixture", "http://127.0.0.1:1/mcp"); err == nil {
		t.Fatal("unreachable server accepted")
	}
	if _, err := os.Stat(filepath.Join(home, "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("failed connection wrote configuration: %v", err)
	}
}
