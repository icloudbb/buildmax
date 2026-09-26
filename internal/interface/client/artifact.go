package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/tool"
)

// artifactResponse is the part of the server's answer a local surface reports.
// The storage key is not in it and never leaves the server.
type artifactResponse struct {
	ID         string         `json:"id"`
	Filename   string         `json:"filename"`
	SizeBytes  int64          `json:"size_bytes"`
	Share      *artifactShare `json:"share,omitempty"`
	ShareError string         `json:"share_error,omitempty"`
}

// artifactShare is the public-link half of an upload that asked for one.
type artifactShare struct {
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
}

// TokenFunc supplies the session credential for serverURL. It is the same seam
// managed inference uses, and it refuses a login belonging to another server.
type TokenFunc func(serverURL string) (string, error)

// artifactPublisher publishes a local file through a BuildMax server on the
// signed-in person's session.
//
// It exists only where there is a server to publish to. A CLI or Desktop
// session running straight against a model provider has no artifact capability
// at all, and its agent is given no artifact tool — see
// docs/design/unified-artifacts.md section 7.1.
type artifactPublisher struct {
	ServerURL string
	// SpaceID is optional. Empty means the server keeps the artifact in the
	// caller's personal space, which is what a client that has never been asked
	// to choose a space should get.
	SpaceID string
	Token   TokenFunc
	HTTP    *http.Client
}

// NewArtifactPublisher returns a publisher, or nil when this surface has no
// server to reach. Returning nil is what leaves the tool unregistered. A nil
// httpClient uses http.DefaultClient.
func NewArtifactPublisher(serverURL, spaceID string, token TokenFunc, httpClient *http.Client) tool.ArtifactPublisher {
	if serverURL == "" || token == nil {
		return nil
	}
	return &artifactPublisher{ServerURL: serverURL, SpaceID: spaceID, Token: token, HTTP: httpClient}
}

// PublishArtifact implements tool.ArtifactPublisher.
func (p *artifactPublisher) PublishArtifact(ctx context.Context, in tool.ArtifactUpload) (tool.PublishedArtifact, error) {
	token, err := p.Token(p.ServerURL)
	if err != nil {
		return tool.PublishedArtifact{}, err
	}
	query := url.Values{}
	if p.SpaceID != "" {
		query.Set("space_id", p.SpaceID)
	}
	if in.Title != "" {
		query.Set("title", in.Title)
	}
	if in.Share {
		query.Set("share", "1")
	}
	endpoint := p.ServerURL + "/api/artifacts"
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	resp, err := httpclient.UploadFile(ctx, p.HTTP, endpoint, token, "file", in.Path, in.Filename)
	if err != nil {
		return tool.PublishedArtifact{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return tool.PublishedArtifact{}, httpclient.DecodeError(resp, "POST "+p.ServerURL+"/api/artifacts")
	}
	var out artifactResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return tool.PublishedArtifact{}, err
	}
	published := tool.PublishedArtifact{
		ArtifactID: out.ID,
		Filename:   out.Filename,
		SizeBytes:  out.SizeBytes,
		URL:        p.ServerURL + "/api/artifacts/" + url.PathEscape(out.ID),
		ShareError: out.ShareError,
	}
	if out.Share != nil {
		published.ShareURL = out.Share.URL
		published.ShareDownloadURL = out.Share.DownloadURL
	}
	return published, nil
}
