package workerclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/tool"
)

// artifactResponse is what the server made of a published file. Only the fields
// a worker reports back to the model are decoded; the storage key is not among
// them and never leaves the server.
type artifactResponse struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
	// URL is the artifact's Portal page, absent when the deployment has no
	// public origin. The worker relays it and never renders one itself: the
	// address it reaches the server at is internal and opens nothing for a
	// person.
	URL        string         `json:"url,omitempty"`
	Share      *artifactShare `json:"share,omitempty"`
	ShareError string         `json:"share_error,omitempty"`
}

// artifactShare is the public-link half of an upload that asked for one. The
// server builds the URLs from its public base URL; the worker only relays them.
type artifactShare struct {
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
}

// artifactPublisher publishes a run's chosen file through the worker API.
//
// The bytes go through the server rather than straight to the object store the
// worker can already reach. That is deliberate: one code path creates
// artifacts, so the size limit, the naming rules, and the write-once ordering
// cannot come to differ between a worker and everyone else — and a worker never
// has to be told which space it is writing to.
type artifactPublisher struct {
	Cfg       WorkerAPIClientConfig
	TaskRunID string
}

// NewArtifactPublisher builds the run-token adapter for the artifact tool.
func NewArtifactPublisher(cfg WorkerAPIClientConfig, taskRunID string) tool.ArtifactPublisher {
	return &artifactPublisher{Cfg: cfg, TaskRunID: taskRunID}
}

// PublishArtifact implements tool.ArtifactPublisher.
func (p *artifactPublisher) PublishArtifact(ctx context.Context, in tool.ArtifactUpload) (tool.PublishedArtifact, error) {
	path := "/api/worker/task-runs/" + url.PathEscape(p.TaskRunID) + "/artifacts"
	query := url.Values{}
	if in.Title != "" {
		query.Set("title", in.Title)
	}
	if in.Share {
		query.Set("share", "1")
	}
	endpoint := p.Cfg.BaseURL + path
	if encoded := query.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	resp, err := httpclient.UploadFile(ctx, p.Cfg.Client, endpoint, p.Cfg.Token, "file", in.Path, in.Filename)
	if err != nil {
		return tool.PublishedArtifact{}, hideServerAddress(err, p.Cfg.BaseURL)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return tool.PublishedArtifact{}, httpclient.DecodeError(resp, "worker API POST "+path)
	}
	var out artifactResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return tool.PublishedArtifact{}, err
	}
	published := tool.PublishedArtifact{
		ArtifactID: out.ID,
		Filename:   out.Filename,
		SizeBytes:  out.SizeBytes,
		URL:        out.URL,
		ShareError: out.ShareError,
	}
	if out.Share != nil {
		published.ShareURL = out.Share.URL
		published.ShareDownloadURL = out.Share.DownloadURL
	}
	return published, nil
}
