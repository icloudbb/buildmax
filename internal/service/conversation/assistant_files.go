package conversation

import (
	"context"
	"fmt"
	"io"
	"mime"
	"slices"
	"strings"
	"unicode/utf8"

	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	"github.com/icloudbb/buildmax/internal/core/llm"
)

// Files reads a Space's files. The artifact service satisfies it; a deleted
// file is absent.
type Files interface {
	Get(ctx context.Context, artifactID string) (*coreartifact.Artifact, error)
	Open(ctx context.Context, rec *coreartifact.Artifact) (io.ReadCloser, error)
}

// An Assistant answers from its readable files in the Server, so what one read
// and one turn may pull into the model is bounded.
const (
	maxReadableFileBytes = 128 << 10
	maxTurnReadBytes     = 384 << 10
)

const (
	toolNameListFiles = "ListFiles"
	toolNameReadFile  = "ReadFile"
)

// notReadable is the one answer for every file outside the allowlist, so a
// requester cannot learn whether an id names something elsewhere.
const notReadable = "No file with that id is available to this assistant. Use ListFiles to see the files you can read."

// textMediaTypes are the non-text/* types read as text.
var textMediaTypes = []string{"application/json", "application/xml", "application/yaml", "application/x-yaml", "application/toml"}

func isTextMediaType(mediaType string) bool {
	base, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		return false
	}
	return strings.HasPrefix(base, "text/") || slices.Contains(textMediaTypes, base)
}

// assistantFiles is one turn's view of an Assistant's readable files: the
// allowlist, in its Space, with the turn's remaining read budget.
type assistantFiles struct {
	files     Files
	spaceID   string
	allowlist []string
	budget    int
}

// file resolves an id on the allowlist to a live file in the Assistant's
// Space, or nil.
func (f *assistantFiles) file(ctx context.Context, id string) *coreartifact.Artifact {
	if !slices.Contains(f.allowlist, id) {
		return nil
	}
	rec, err := f.files.Get(ctx, id)
	if err != nil || rec == nil || rec.SpaceID != f.spaceID {
		return nil
	}
	return rec
}

func (f *assistantFiles) list(ctx context.Context) string {
	var lines []string
	for _, id := range f.allowlist {
		rec := f.file(ctx, id)
		if rec == nil {
			continue
		}
		line := fmt.Sprintf("%d. %s | %s | %d bytes", len(lines)+1, rec.ID, rec.Filename, rec.SizeBytes)
		switch {
		case !isTextMediaType(rec.MediaType):
			line += " | not readable: not a text file"
		case rec.SizeBytes > maxReadableFileBytes:
			line += " | not readable: too large"
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return "This assistant has no files to read."
	}
	return "file_id | name | size\n" + strings.Join(lines, "\n")
}

func (f *assistantFiles) read(ctx context.Context, id string) (string, error) {
	rec := f.file(ctx, id)
	if rec == nil {
		return notReadable, nil
	}
	if !isTextMediaType(rec.MediaType) {
		return fmt.Sprintf("%s is not a text file (%s), so it cannot be read here.", rec.Filename, rec.MediaType), nil
	}
	if rec.SizeBytes > maxReadableFileBytes {
		return fmt.Sprintf("%s is too large to read here (%d bytes; the limit is %d).", rec.Filename, rec.SizeBytes, maxReadableFileBytes), nil
	}
	if rec.SizeBytes > int64(f.budget) {
		return "This turn has already read as much file content as it may. Answer from what you have read.", nil
	}
	body, err := f.files.Open(ctx, rec)
	if err != nil {
		return "", err
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, maxReadableFileBytes+1))
	if err != nil {
		return "", err
	}
	// The recorded size is the upload's; the bytes decide.
	if len(data) > maxReadableFileBytes || len(data) > f.budget {
		return fmt.Sprintf("%s is too large to read here.", rec.Filename), nil
	}
	if !utf8.Valid(data) {
		return fmt.Sprintf("%s is not valid text, so it cannot be read here.", rec.Filename), nil
	}
	f.budget -= len(data)
	return fmt.Sprintf("file: %s\n\n%s", rec.Filename, data), nil
}

type listFilesTool struct{ files *assistantFiles }

func (t *listFilesTool) Name() string { return toolNameListFiles }
func (t *listFilesTool) Description() string {
	return "List the Space files you may read to answer: their file_id, name, and size. Only text files up to a size limit can be read."
}
func (t *listFilesTool) Parameters() any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}
func (t *listFilesTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	return t.files.list(ctx), nil
}

type readFileTool struct{ files *assistantFiles }

func (t *readFileTool) Name() string { return toolNameReadFile }
func (t *readFileTool) Description() string {
	return "Read one of your files by the file_id ListFiles gives. Answer from its content; quote only what the person needs."
}
func (t *readFileTool) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_id": map[string]any{"type": "string", "description": "The file_id from ListFiles."},
		},
		"required": []any{"file_id"},
	}
}
func (t *readFileTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	id, _ := args["file_id"].(string)
	if strings.TrimSpace(id) == "" {
		return "file_id is required. Use ListFiles to see the files you can read.", nil
	}
	return t.files.read(ctx, strings.TrimSpace(id))
}

// newAssistantFileTools returns ListFiles and ReadFile over the allowlist, or
// nothing when the Assistant has no readable files or the deployment no file
// storage.
func newAssistantFileTools(files Files, spaceID string, allowlist []string) []llm.Tool {
	if files == nil || len(allowlist) == 0 {
		return nil
	}
	f := &assistantFiles{files: files, spaceID: spaceID, allowlist: allowlist, budget: maxTurnReadBytes}
	return []llm.Tool{&listFilesTool{files: f}, &readFileTool{files: f}}
}
