package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// UploadedArtifact is what a caller needs after publishing a file: the id to
// reference it by, and the public link if one was asked for.
type UploadedArtifact struct {
	ID         string
	Filename   string
	SizeBytes  int64
	ShareURL   string
	ShareError string
}

// PublishArtifact uploads a local file as an artifact and returns its id.
//
// It posts to the default-space route with an optional space_id, so a caller
// that has not chosen a space gets its personal one — the same default the
// agent's artifact tool uses. The id it returns is the exact handle to list
// after `Artifacts:` in an issue comment; there is no separate reference form.
// An empty spaceID means the caller's personal space; empty title lets the
// server derive one.
func (c *Client) PublishArtifact(ctx context.Context, token, spaceID, title, path, filename string, share bool) (UploadedArtifact, error) {
	q := url.Values{}
	if spaceID != "" {
		q.Set("space_id", spaceID)
	}
	if title != "" {
		q.Set("title", title)
	}
	if share {
		q.Set("share", "1")
	}
	endpoint := c.BaseURL + "/api/artifacts"
	if enc := q.Encode(); enc != "" {
		endpoint += "?" + enc
	}
	resp, err := httpclient.UploadFile(ctx, c.HTTPClient, endpoint, token, "file", path, filename)
	if err != nil {
		return UploadedArtifact{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return UploadedArtifact{}, httpclient.DecodeError(resp, "POST /api/artifacts")
	}
	var out artifactResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return UploadedArtifact{}, err
	}
	up := UploadedArtifact{ID: out.ID, Filename: out.Filename, SizeBytes: out.SizeBytes, ShareError: out.ShareError}
	if out.Share != nil {
		up.ShareURL = out.Share.URL
	}
	return up, nil
}
