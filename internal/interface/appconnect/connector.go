// Package appconnect implements the local, deliberately small HTTP connector prototype.
package appconnect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coreconnect "github.com/icloudbb/buildmax/internal/core/appconnect"
	"github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"
)

// Load reads the connector declaration from an installed plugin.
func Load(pluginDir string) (coreconnect.Manifest, error) {
	data, err := os.ReadFile(filepath.Join(pluginDir, coreconnect.ManifestFile))
	if err != nil {
		return coreconnect.Manifest{}, err
	}
	return coreconnect.Parse(data)
}

func OAuthConfig(m coreconnect.Manifest, redirectURL string) (oauth2.Config, error) {
	clientID := os.Getenv(m.Auth.ClientIDEnv)
	if clientID == "" {
		return oauth2.Config{}, fmt.Errorf("set %s to this app's OAuth client ID", m.Auth.ClientIDEnv)
	}
	return oauth2.Config{
		ClientID: clientID, RedirectURL: redirectURL, Scopes: m.Auth.Scopes,
		Endpoint: oauth2.Endpoint{AuthURL: m.Auth.AuthorizationURL, TokenURL: m.Auth.TokenURL},
	}, nil
}

// SaveToken stores one local account per plugin. File storage is an explicit
// portability mode; the default requires an OS credential store.
func SaveToken(name string, token *oauth2.Token) error {
	if err := plugin.ValidateName(name); err != nil {
		return err
	}
	raw, err := json.Marshal(token)
	if err != nil {
		return err
	}
	if os.Getenv(config.EnvKeyBuildmaxCredentialStore) == "file" {
		file := tokenFile(name)
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		tmp, err := os.CreateTemp(filepath.Dir(file), ".connection-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		if _, err := tmp.Write(raw); err != nil {
			tmp.Close()
			return err
		}
		if err := tmp.Close(); err != nil {
			return err
		}
		return os.Rename(tmp.Name(), file)
	}
	return keyring.Set("BuildMax App Connections", config.DataDir()+":"+name, string(raw))
}

func LoadToken(name string) (*oauth2.Token, error) {
	if err := plugin.ValidateName(name); err != nil {
		return nil, err
	}
	var raw []byte
	var err error
	if os.Getenv(config.EnvKeyBuildmaxCredentialStore) == "file" {
		raw, err = os.ReadFile(tokenFile(name))
	} else {
		var value string
		value, err = keyring.Get("BuildMax App Connections", config.DataDir()+":"+name)
		raw = []byte(value)
	}
	if err != nil {
		return nil, fmt.Errorf("connection %s unavailable: %w", name, err)
	}
	var token oauth2.Token
	if err := json.Unmarshal(raw, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func tokenFile(name string) string {
	return filepath.Join(config.DataDir(), "connections", name+".json")
}

// Call never accepts a caller-selected URL, method, or credential. A write
// requires the interactive caller to have completed confirmation first.
func Call(ctx context.Context, name string, m coreconnect.Manifest, opName string, body []byte, approved bool) ([]byte, error) {
	op, ok := m.Operations[opName]
	if !ok {
		return nil, fmt.Errorf("operation %q is not declared", opName)
	}
	if op.Effect == "write" && !approved {
		return nil, errors.New("write operation needs interactive confirmation")
	}
	if op.Effect == "read" && len(body) != 0 {
		return nil, errors.New("read operation cannot have a body")
	}
	if len(body) > 1<<20 {
		return nil, errors.New("request body exceeds 1 MiB")
	}
	if op.Effect == "write" && !json.Valid(body) {
		return nil, errors.New("write body must be JSON")
	}
	token, err := LoadToken(name)
	if err != nil {
		return nil, err
	}
	cfg, err := OAuthConfig(m, "")
	if err != nil {
		return nil, err
	}
	url := strings.TrimSuffix(m.APIOrigin, "/") + op.Path
	var reader io.Reader
	if op.Effect == "write" {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, op.Method, url, reader)
	if err != nil {
		return nil, err
	}
	if op.Effect == "write" {
		req.Header.Set("Content-Type", "application/json")
	}
	base := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	source := cfg.TokenSource(ctx, token)
	fresh, err := source.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh connection: %w", err)
	}
	if fresh.AccessToken != token.AccessToken || fresh.RefreshToken != token.RefreshToken || !fresh.Expiry.Equal(token.Expiry) {
		if err := SaveToken(name, fresh); err != nil {
			return nil, fmt.Errorf("save refreshed connection: %w", err)
		}
	}
	req.Header.Set("Authorization", "Bearer "+fresh.AccessToken)
	resp, err := base.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	result, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
	if err != nil {
		return nil, err
	}
	if len(result) > 1<<20 {
		return nil, errors.New("response exceeds 1 MiB")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("app API returned HTTP %d: %s", resp.StatusCode, result)
	}
	return result, nil
}
