package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/icloudbb/buildmax/internal/util"
)

// ArtifactUpload is one file an agent chose to publish.
type ArtifactUpload struct {
	// Path is the resolved absolute path of a regular, readable file.
	Path string
	// Filename is the name the artifact carries, already reduced to one element.
	Filename string
	Title    string
	// Share asks the server to also create a public link and return it, so an
	// agent can hand a person one link that opens and renders.
	Share bool
}

// PublishedArtifact is what the artifact service made of it.
type PublishedArtifact struct {
	ArtifactID string
	Filename   string
	SizeBytes  int64
	// URL is where an authorized person opens it. Empty when the surface knows
	// the id but not the address to render it at.
	URL string
	// ShareURL is a public link anyone can open, set only when Share was asked
	// for and the server created one. ShareDownloadURL is its raw-download form.
	ShareURL         string
	ShareDownloadURL string
	// ShareError explains why a requested link could not be made. The artifact
	// still published; only the link is missing.
	ShareError string
}

// ArtifactPublisher hands one local file to the artifact service.
//
// A port rather than the capability itself: this package must not know whether
// the file reaches a server over a person's session or over a run token, and
// must not grow a dependency on the service to find out. A surface that has no
// implementation does not register the tool — see docs/design/unified-artifacts.md
// section 7.1.
type ArtifactPublisher interface {
	PublishArtifact(ctx context.Context, in ArtifactUpload) (PublishedArtifact, error)
}

// UploadArtifact publishes one file the agent names as a durable artifact.
type UploadArtifact struct {
	workspaceTool
	publisher ArtifactPublisher
}

// NewUploadArtifact creates the tool. It is only ever constructed where a
// publisher exists; see agentapp's tool assembly.
func NewUploadArtifact(ws util.Workspace, publisher ArtifactPublisher) *UploadArtifact {
	return &UploadArtifact{workspaceTool: workspaceTool{ws: ws}, publisher: publisher}
}

func (t *UploadArtifact) Name() string { return ToolNameUploadArtifact }

// Description states the choosing rule, not just the mechanics.
//
// Without it the tool is used as a save button and the space's artifact list
// fills with intermediate files. What the model has to understand is that this
// publishes, once, the file a person is meant to receive.
func (t *UploadArtifact) Description() string {
	return "Publish one finished file as a BuildMax artifact so people can open, keep, and " +
		"share it. Use it for the deliverable of your work — the report, the export, the " +
		"generated document someone asked for — and cite the returned reference in your final " +
		"answer. Upload the final file only: not intermediate output, not build products, not " +
		"caches, and never a file holding credentials or configuration such as .env. Content is " +
		"immutable, so publishing a corrected version creates a second artifact; publish once " +
		"you are done rather than at each step."
}

func (t *UploadArtifact) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type": "string",
				"description": "Path to the file to publish, absolute or relative to the " +
					"workspace. It must be a regular readable file inside the workspace.",
			},
			"title": map[string]any{
				"type": "string",
				"description": "Optional short label for people reading a list of artifacts. " +
					"The filename is still the content's name.",
			},
			"purpose": map[string]any{
				"type": "string",
				"description": "Optional one-line note on what this file is for, shown alongside " +
					"it. Not a place for file contents.",
			},
			"share": map[string]any{
				"type": "boolean",
				"description": "Set true to also create a public link anyone can open without a " +
					"BuildMax login, and receive it in the result. Use it when the person should " +
					"be able to open the file — a report to read, an HTML prototype to view — from " +
					"a link you give them. The link is revocable and expires. Defaults to false: " +
					"without it the artifact is reachable only by space members.",
			},
		},
		"required": []string{"path"},
	}
}

// maxArtifactTitleLen bounds both caller-supplied strings. They land in durable
// metadata, which is not a place for an unbounded value.
const maxArtifactTitleLen = 200

// Execute validates the file, publishes it, and reports the reference.
func (t *UploadArtifact) Execute(ctx context.Context, args map[string]any) (string, error) {
	path, err := parseRequiredString(args, "path")
	if err != nil {
		return "", err
	}
	resolved, err := t.resolvePublishablePath(path)
	if err != nil {
		return "", err
	}
	if t.publisher == nil {
		// Registered without a publisher would be a wiring bug, and saying so is
		// better than a nil dereference the model reads as a transient failure.
		return "", errors.New("artifact publishing is not available in this session")
	}

	title := bounded(parseOptionalString(args, "title", ""), maxArtifactTitleLen)
	purpose := bounded(parseOptionalString(args, "purpose", ""), maxArtifactTitleLen)
	if title == "" {
		title = purpose
	}

	published, err := t.publisher.PublishArtifact(ctx, ArtifactUpload{
		Path:     resolved,
		Filename: filepath.Base(resolved),
		Title:    title,
		Share:    parseOptionalBool(args, "share", false),
	})
	if err != nil {
		return "", err
	}
	return formatPublishedArtifact(published, purpose), nil
}

// resolvePublishablePath refuses anything that is not a plain file the caller
// may publish.
//
// The symlink check is the one that matters here and is not the workspace
// check: containment is decided lexically, so a link inside the workspace can
// still name /etc/ssh/id_rsa, and publishing sends the target's bytes to a space.
func (t *UploadArtifact) resolvePublishablePath(path string) (string, error) {
	if _, err := t.resolveFilePath(path); err != nil {
		return "", err
	}
	real, err := util.ResolveRealPath(t.root(), path)
	if errors.Is(err, util.ErrPathOutsideRoot) {
		return "", fmt.Errorf("%s resolves outside the workspace and cannot be published", path)
	}
	if err != nil {
		return "", normalizeOSError(err)
	}
	info, err := os.Lstat(real)
	if err != nil {
		return "", normalizeOSError(err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file, so there is nothing to publish", path)
	}
	return real, nil
}

func bounded(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		return s[:max]
	}
	return s
}

// formatPublishedArtifact tells the model what to cite. The id leads because it
// is the reference that means the same thing everywhere; the URL is one
// surface's rendering of it.
func formatPublishedArtifact(a PublishedArtifact, purpose string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Published %s as artifact %s (%d bytes).", a.Filename, a.ArtifactID, a.SizeBytes)
	if a.URL != "" {
		fmt.Fprintf(&b, "\nURL: %s", a.URL)
	}
	if purpose != "" {
		fmt.Fprintf(&b, "\nPurpose: %s", purpose)
	}
	switch {
	case a.ShareURL != "":
		// The public link is what a person opens, so it leads the closing
		// instruction rather than the space-only reference.
		fmt.Fprintf(&b, "\nPublic link (opens without a login): %s", a.ShareURL)
		if a.ShareDownloadURL != "" {
			fmt.Fprintf(&b, "\nDownload: %s", a.ShareDownloadURL)
		}
		b.WriteString("\nCite the public link in your final answer so the person can open the file.")
	case a.ShareError != "":
		// The file published; only the link failed. Say both, so the model
		// reports what happened rather than inventing a link.
		fmt.Fprintf(&b, "\nA public link was requested but could not be created: %s", a.ShareError)
		b.WriteString("\nCite the artifact reference in your final answer; there is no public link.")
	default:
		b.WriteString("\nCite this reference in your final answer so the person can find the file.")
	}
	return b.String()
}
