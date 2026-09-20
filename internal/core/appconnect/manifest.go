// Package appconnect owns the bounded, public connector manifest format.
package appconnect

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const ManifestFile = "connector.yaml"

var operationName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Manifest contains no credentials or Agent grants.
type Manifest struct {
	Auth struct {
		AuthorizationURL string   `yaml:"authorization_url"`
		TokenURL         string   `yaml:"token_url"`
		ClientIDEnv      string   `yaml:"client_id_env"`
		Scopes           []string `yaml:"scopes"`
	} `yaml:"auth"`
	APIOrigin  string               `yaml:"api_origin"`
	Operations map[string]Operation `yaml:"operations"`
}

type Operation struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
	Effect string `yaml:"effect"`
}

func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if len(data) > 64<<10 {
		return m, errors.New("connector.yaml exceeds 64 KiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&m); err != nil {
		return m, fmt.Errorf("connector.yaml: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return m, errors.New("connector.yaml must contain one document")
	} else if err != io.EOF {
		return m, fmt.Errorf("connector.yaml: %w", err)
	}
	for _, raw := range []string{m.Auth.AuthorizationURL, m.Auth.TokenURL, m.APIOrigin} {
		if err := validURL(raw); err != nil {
			return m, err
		}
	}
	api, _ := url.Parse(m.APIOrigin)
	if api.Path != "" && api.Path != "/" || api.RawQuery != "" || api.Fragment != "" {
		return m, errors.New("api_origin must be an origin without path, query or fragment")
	}
	if !envName.MatchString(m.Auth.ClientIDEnv) || len(m.Auth.Scopes) == 0 || len(m.Operations) == 0 {
		return m, errors.New("client_id_env, scopes and operations are required")
	}
	for _, scope := range m.Auth.Scopes {
		if strings.TrimSpace(scope) == "" {
			return m, errors.New("scope must not be empty")
		}
	}
	for name, op := range m.Operations {
		if !operationName.MatchString(name) {
			return m, fmt.Errorf("invalid operation name %q", name)
		}
		if op.Method != http.MethodGet && op.Method != http.MethodPost {
			return m, fmt.Errorf("%s: only GET and POST are supported", name)
		}
		if op.Effect != "read" && op.Effect != "write" || (op.Effect == "read") != (op.Method == http.MethodGet) {
			return m, fmt.Errorf("%s: GET must be read and POST must be write", name)
		}
		if !strings.HasPrefix(op.Path, "/") || strings.ContainsAny(op.Path, "?#\\%") || path.Clean(op.Path) != op.Path || strings.Contains(op.Path, "//") {
			return m, fmt.Errorf("%s: path must be a fixed clean absolute path", name)
		}
	}
	return m, nil
}

func validURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("invalid connector URL %q", raw)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost")) {
		return fmt.Errorf("connector URL must use HTTPS (loopback HTTP is allowed): %q", raw)
	}
	return nil
}
