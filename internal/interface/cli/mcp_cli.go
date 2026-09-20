package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coremcp "github.com/icloudbb/buildmax/internal/core/mcp"
	"github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/infra/mcp"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newConnectMCPCommand() *cobra.Command {
	c := &cobra.Command{
		Use: "mcp <name> <url>", Short: "Register and check a remote MCP server",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, rawURL := args[0], args[1]
			if err := plugin.ValidateName(name); err != nil {
				return err
			}
			if err := validateMCPURL(rawURL); err != nil {
				return err
			}
			transport, _ := cmd.Flags().GetString("transport")
			bearerEnv, _ := cmd.Flags().GetString("bearer-env")
			entry := coremcp.ServerConfig{Type: transport, URL: rawURL, BearerTokenEnv: bearerEnv}
			if err := coremcp.ValidateServerConfig(name, entry); err != nil {
				return err
			}
			path := filepath.Join(config.DataDir(), "mcp.json")
			doc, servers, err := readMCPDocument(path)
			if err != nil {
				return err
			}
			if _, exists := servers[name]; exists {
				return fmt.Errorf("MCP server %q already exists in %s", name, path)
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			registry, err := mcp.NewRegistry(ctx, &coremcp.ConfigRoot{MCPServers: map[string]coremcp.ServerConfig{name: entry}}, nil)
			if err != nil {
				return fmt.Errorf("MCP connection check failed: %w", err)
			}
			defer registry.Close()
			if err := saveMCPDocument(path, doc, servers, name, entry); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Connected MCP server %q. Saved to %s.\n%s\n", name, path, registry.Catalog())
			return nil
		},
	}
	c.Flags().String("transport", coremcp.TransportHTTP, "MCP transport: http or sse")
	c.Flags().String("bearer-env", "", "environment variable containing a Bearer token; the token is not saved")
	return c
}

func validateMCPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("MCP URL must have a host and no user info, query, or fragment")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback()) {
		return nil
	}
	return errors.New("MCP URL must use HTTPS, except for a loopback test server")
}

func readMCPDocument(path string) (map[string]json.RawMessage, map[string]json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if err == nil {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, nil, fmt.Errorf("invalid MCP config %s: %v", path, err)
		}
		if doc == nil {
			return nil, nil, fmt.Errorf("invalid MCP config %s: expected an object", path)
		}
	}
	servers := map[string]json.RawMessage{}
	if existing, ok := doc["mcpServers"]; ok {
		if err := json.Unmarshal(existing, &servers); err != nil {
			return nil, nil, fmt.Errorf("invalid mcpServers in %s: %v", path, err)
		}
		if servers == nil {
			return nil, nil, fmt.Errorf("invalid mcpServers in %s: expected an object", path)
		}
	}
	return doc, servers, nil
}

func saveMCPDocument(path string, doc, servers map[string]json.RawMessage, name string, entry coremcp.ServerConfig) error {
	rawEntry, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	servers[name] = rawEntry
	if doc["mcpServers"], err = json.Marshal(servers); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mcp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func newMCPCommand() *cobra.Command {
	c := &cobra.Command{Use: "mcp", Short: "Discover and call connected MCP servers"}
	c.AddCommand(&cobra.Command{Use: "list", Short: "List configured MCP servers", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := resolvedMCPConfig()
			if err != nil {
				return err
			}
			if cfg == nil {
				fmt.Fprintln(cmd.OutOrStdout(), "No MCP servers configured.")
				return nil
			}
			names := make([]string, 0, len(cfg.MCPServers))
			for name := range cfg.MCPServers {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				entry := cfg.MCPServers[name]
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", name, entry.Type)
			}
			return nil
		},
	})
	c.AddCommand(&cobra.Command{Use: "tools <server>", Short: "List one server's tools", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMCPRegistry(cmd.Context(), args[0], func(_ context.Context, reg *mcp.Registry) error {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), reg.Catalog())
				return err
			})
		},
	})
	c.AddCommand(&cobra.Command{Use: "schema <server> <tool>", Short: "Show one tool's input schema", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withMCPRegistry(cmd.Context(), args[0], func(_ context.Context, reg *mcp.Registry) error {
				detail, err := reg.ToolSchemaDetail(args[0], args[1])
				if err != nil {
					return err
				}
				_, err = io.WriteString(cmd.OutOrStdout(), detail)
				return err
			})
		},
	})
	call := &cobra.Command{Use: "call <server> <tool>", Short: "Call one MCP tool", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := cmd.Flags().GetString("json")
			if len(raw) > 1<<20 {
				return errors.New("MCP arguments exceed 1 MiB")
			}
			var input map[string]any
			if err := json.Unmarshal([]byte(raw), &input); err != nil || input == nil {
				return errors.New("MCP arguments must be a JSON object")
			}
			return withMCPRegistry(cmd.Context(), args[0], func(ctx context.Context, reg *mcp.Registry) error {
				if _, err := reg.ToolSchemaDetail(args[0], args[1]); err != nil {
					return err
				}
				if !reg.ToolIsReadOnly(args[0], args[1]) {
					preview, err := json.MarshalIndent(input, "", "  ")
					if err != nil {
						return err
					}
					if err := confirmMCPCall(cmd.ErrOrStderr(), args[0], args[1], string(preview)); err != nil {
						return err
					}
				}
				result, err := reg.CallMcp(ctx, args[0], args[1], input)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), result)
				return err
			})
		},
	}
	call.Flags().String("json", "{}", "JSON object of tool arguments")
	c.AddCommand(call)
	return c
}

func resolvedMCPConfig() (*coremcp.ConfigRoot, error) {
	workspace, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	res, err := config.ResolveMCPConfig(workspace, config.DiscoverPlugins().Loadable())
	return res.Config, err
}

func withMCPRegistry(parent context.Context, name string, use func(context.Context, *mcp.Registry) error) error {
	cfg, err := resolvedMCPConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("no MCP servers configured")
	}
	entry, ok := cfg.MCPServers[name]
	if !ok {
		return fmt.Errorf("MCP server %q is not configured", name)
	}
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	reg, err := mcp.NewRegistry(ctx, &coremcp.ConfigRoot{MCPServers: map[string]coremcp.ServerConfig{name: entry}}, nil)
	if err != nil {
		return err
	}
	defer reg.Close()
	return use(ctx, reg)
}

func confirmMCPCall(w io.Writer, server, tool, args string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("MCP tool is not marked read-only; call requires an interactive terminal")
	}
	want := server + "/" + tool
	fmt.Fprintf(w, "Call MCP tool %s with arguments:\n%s\nType %s to confirm: ", want, args, want)
	var got string
	if _, err := fmt.Fscanln(os.Stdin, &got); err != nil {
		return err
	}
	if strings.TrimSpace(got) != want {
		return errors.New("MCP call cancelled")
	}
	return nil
}
