package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/infra/pluginwire"
)

// ListPlugins returns the deployment's browsable catalog.
func (c *Client) ListPlugins(ctx context.Context, token string) ([]coreplugin.Plugin, error) {
	var out pluginwire.CatalogResponse
	if err := c.getJSON(ctx, token, pluginwire.CatalogPath, &out); err != nil {
		return nil, err
	}
	return out.Plugins, nil
}

// GetPlugin returns one entry and every release published under it, withdrawn
// ones included and marked.
func (c *Client) GetPlugin(ctx context.Context, token, name string) (*pluginwire.PluginResponse, error) {
	var out pluginwire.PluginResponse
	path := fmt.Sprintf(pluginwire.PluginPath, url.PathEscape(name))
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListSpaceActivations returns what a space has activated for its background
// runs, and who fills that list.
//
// It is a read only. Changing an activation stays in Portal, where the audit
// trail and the space's other shared automation already are; what this serves is
// somebody debugging a run who wants the answer without a browser.
func (c *Client) ListSpaceActivations(ctx context.Context, token, spaceID string) (*pluginwire.ActivationsResponse, error) {
	var out pluginwire.ActivationsResponse
	path := fmt.Sprintf(pluginwire.SpaceActivationsPath, url.PathEscape(spaceID))
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadRelease streams one release's bytes into w and returns the digest the
// server sent with them.
//
// The bytes are copied straight through rather than collected: a package is
// bounded, but bounded at tens of megabytes, and the caller is writing to a
// staging file anyway.
func (c *Client) DownloadRelease(ctx context.Context, token, name, version string, allowYanked bool, w io.Writer) (string, error) {
	path := fmt.Sprintf(pluginwire.DownloadPath, url.PathEscape(name), url.PathEscape(version))
	if allowYanked {
		path += "?" + pluginwire.QueryAllowYanked + "=true"
	}
	resp, err := c.do(ctx, http.MethodGet, token, path, "", nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", httpclient.DecodeError(resp, "")
	}
	if _, err := io.Copy(w, resp.Body); err != nil {
		return "", fmt.Errorf("download interrupted: %w", err)
	}
	return resp.Header.Get(pluginwire.DigestHeader), nil
}

// PublishRelease uploads one package.
//
// The body is the archive itself, streamed from the reader, and the claim about
// where it came from travels beside it. The server hashes and inspects what it
// receives, so nothing here is trusted on the far side.
func (c *Client) PublishRelease(
	ctx context.Context, token, name string, body io.Reader, source coreplugin.ReleaseSource,
) (*coreplugin.Release, error) {
	q := url.Values{}
	setIfPresent(q, pluginwire.QuerySourceRemote, source.RemoteURL)
	setIfPresent(q, pluginwire.QuerySourceCommit, source.Commit)
	setIfPresent(q, pluginwire.QuerySourceBranch, source.Branch)
	if source.Dirty {
		q.Set(pluginwire.QuerySourceDirty, strconv.FormatBool(true))
	}
	path := fmt.Sprintf(pluginwire.AdminReleasesPath, url.PathEscape(name))
	if len(q) > 0 {
		path += "?" + q.Encode()
	}

	resp, err := c.do(ctx, http.MethodPost, token, path, "application/gzip", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out coreplugin.Release
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// YankRelease withdraws one release from default selection.
func (c *Client) YankRelease(ctx context.Context, token, name, version, reason string) error {
	payload, err := json.Marshal(pluginwire.ReleaseStateRequest{Yanked: true, Reason: reason})
	if err != nil {
		return err
	}
	path := fmt.Sprintf(pluginwire.AdminReleaseStatePath, url.PathEscape(name), url.PathEscape(version))
	return c.sendNoContent(ctx, http.MethodPut, token, path, payload)
}

// SetPluginArchived retires or restores a catalog entry.
func (c *Client) SetPluginArchived(ctx context.Context, token, name string, archived bool) error {
	payload, err := json.Marshal(pluginwire.PluginStateRequest{Archived: archived})
	if err != nil {
		return err
	}
	path := fmt.Sprintf(pluginwire.AdminPluginStatePath, url.PathEscape(name))
	return c.sendNoContent(ctx, http.MethodPut, token, path, payload)
}

func (c *Client) getJSON(ctx context.Context, token, path string, dst any) error {
	resp, err := c.do(ctx, http.MethodGet, token, path, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return httpclient.DecodeError(resp, "")
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) sendNoContent(ctx context.Context, method, token, path string, payload []byte) error {
	var body io.Reader
	contentType := ""
	if payload != nil {
		body, contentType = bytes.NewReader(payload), "application/json"
	}
	resp, err := c.do(ctx, method, token, path, contentType, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return httpclient.DecodeError(resp, "")
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, token, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, nil
}

func setIfPresent(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}
