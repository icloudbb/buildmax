// Package mcp holds the shape of an mcp.json document and the rules a server
// definition must satisfy.
//
// It is the contract half of the pair whose implementation is internal/infra/mcp,
// the same split as internal/core/llm and internal/infra/llm. It sits in core
// because two callers that cannot share anything higher both need it: loading a
// file for a run, and inspecting an uploaded plugin package before publishing
// it, which internal/server does and which may not import internal/config.
package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Transport names for a server entry's "type" field.
const (
	TransportStdio = "stdio"
	TransportSSE   = "sse"
	TransportHTTP  = "http"
)

// ConfigRoot is the root of mcp.json (top-level key mcpServers).
type ConfigRoot struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

// ServerConfig is one entry in mcpServers.
type ServerConfig struct {
	Type           string            `json:"type"`
	Command        string            `json:"command,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	URL            string            `json:"url,omitempty"`
	BearerTokenEnv string            `json:"bearer_token_env,omitempty"`
}

var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseConfig decodes an mcp.json document without validating its entries.
//
// A document declaring no server returns (nil, nil): an empty file and a
// missing one mean the same thing to every caller, and neither is an error.
func ParseConfig(data []byte) (*ConfigRoot, error) {
	var root ConfigRoot
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.MCPServers) == 0 {
		return nil, nil
	}
	return &root, nil
}

// ValidateServerConfig reports why an entry could not start.
//
// It runs against the document as written, so it must not depend on variable
// expansion: a command of "${BUILDMAX_PLUGIN_ROOT}/bin/server" is present, and
// whether that path exists is a question for the machine that runs it.
func ValidateServerConfig(id string, s ServerConfig) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("mcp: empty server id in mcpServers")
	}
	switch s.Type {
	case TransportStdio:
		if strings.TrimSpace(s.Command) == "" {
			return fmt.Errorf("mcp server %q: stdio requires command", id)
		}
		if s.BearerTokenEnv != "" {
			return fmt.Errorf("mcp server %q: bearer_token_env requires http or sse", id)
		}
	case TransportSSE, TransportHTTP:
		if strings.TrimSpace(s.URL) == "" {
			return fmt.Errorf("mcp server %q: %s requires url", id, s.Type)
		}
		if s.BearerTokenEnv != "" && !envNamePattern.MatchString(s.BearerTokenEnv) {
			return fmt.Errorf("mcp server %q: invalid bearer_token_env name", id)
		}
	default:
		return fmt.Errorf("mcp server %q: invalid type %q (want stdio, sse, or http)", id, s.Type)
	}
	return nil
}
