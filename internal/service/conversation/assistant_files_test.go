package conversation

import (
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
)

// fakeFiles serves artifacts and their bytes; a missing one is an error, as
// the artifact service reports a deleted file.
type fakeFiles struct {
	recs    map[string]coreartifact.Artifact
	content map[string]string
	opened  []string
}

func (f *fakeFiles) Get(_ context.Context, id string) (*coreartifact.Artifact, error) {
	rec, ok := f.recs[id]
	if !ok {
		return nil, errors.New("artifact not found")
	}
	return &rec, nil
}

func (f *fakeFiles) Open(_ context.Context, rec *coreartifact.Artifact) (io.ReadCloser, error) {
	f.opened = append(f.opened, rec.ID)
	return io.NopCloser(strings.NewReader(f.content[rec.ID])), nil
}

func (f *fakeFiles) add(id, space, name, mediaType, content string) {
	f.recs[id] = coreartifact.Artifact{ID: id, SpaceID: space, Filename: name, MediaType: mediaType, SizeBytes: int64(len(content))}
	f.content[id] = content
}

func newFakeFiles() *fakeFiles {
	f := &fakeFiles{recs: map[string]coreartifact.Artifact{}, content: map[string]string{}}
	f.add("af_policy", asstSpace, "leave-policy.md", "text/markdown; charset=utf-8", "Staff get 25 days of leave.")
	f.add("af_handbook", asstSpace, "handbook.json", "application/json", `{"probation_months":3}`)
	f.add("af_logo", asstSpace, "logo.png", "image/png", "\x89PNG")
	f.add("af_huge", asstSpace, "dump.txt", "text/plain", strings.Repeat("x", maxReadableFileBytes+1))
	f.add("af_garbled", asstSpace, "bad.txt", "text/plain", "\xff\xfe\xfd")
	f.add("af_salaries", asstSpace, "salaries.csv", "text/csv", "SECRET salary table")
	f.add("af_elsewhere", "tm_other", "other.md", "text/markdown", "SECRET other space")
	return f
}

func fileTools(t *testing.T, files Files, allowlist ...string) (list, read func(id string) string) {
	t.Helper()
	tools := newAssistantFileTools(files, asstSpace, allowlist)
	if len(tools) != 2 {
		t.Fatalf("tools = %d", len(tools))
	}
	run := func(i int, args map[string]any) string {
		out, err := tools[i].Execute(context.Background(), args)
		if err != nil {
			t.Fatalf("%s: %v", tools[i].Name(), err)
		}
		return out
	}
	return func(string) string { return run(0, nil) }, func(id string) string { return run(1, map[string]any{"file_id": id}) }
}

// Only files on the allowlist and in the Assistant's Space can be listed or
// read; anything else, in or outside the Space, gets the same answer as an id
// that names nothing.
func TestAssistantFilesReadOnlyTheAllowlist(t *testing.T) {
	files := newFakeFiles()
	list, read := fileTools(t, files, "af_policy", "af_handbook", "af_logo", "af_huge", "af_garbled", "af_elsewhere", "af_deleted")

	listing := list("")
	for _, want := range []string{"af_policy", "leave-policy.md", "af_handbook", "logo.png | 4 bytes | not readable: not a text file", "not readable: too large"} {
		if !strings.Contains(listing, want) {
			t.Errorf("listing lacks %q:\n%s", want, listing)
		}
	}
	for _, hidden := range []string{"salaries", "af_elsewhere", "af_deleted"} {
		if strings.Contains(listing, hidden) {
			t.Errorf("listing shows %q:\n%s", hidden, listing)
		}
	}

	if got := read("af_policy"); !strings.Contains(got, "Staff get 25 days of leave.") {
		t.Errorf("read policy = %q", got)
	}
	if got := read("af_handbook"); !strings.Contains(got, "probation_months") {
		t.Errorf("read JSON = %q", got)
	}
	for _, id := range []string{"af_salaries", "af_elsewhere", "af_deleted", "leave-policy.md", "../af_salaries"} {
		if got := read(id); got != notReadable {
			t.Errorf("read %q = %q", id, got)
		}
	}
	if slices.Contains(files.opened, "af_salaries") || slices.Contains(files.opened, "af_elsewhere") {
		t.Errorf("opened %v", files.opened)
	}
}

// Binary, oversized, and invalid text are refused with a reason the model
// can relay, and their bytes never reach it.
func TestAssistantFilesRefuseWhatIsNotReadableText(t *testing.T) {
	files := newFakeFiles()
	_, read := fileTools(t, files, "af_logo", "af_huge", "af_garbled")
	for id, want := range map[string]string{
		"af_logo":    "not a text file",
		"af_huge":    "too large",
		"af_garbled": "not valid text",
	} {
		if got := read(id); !strings.Contains(got, want) || strings.Contains(got, "PNG") || strings.Contains(got, "xxxx") {
			t.Errorf("read %s = %q, want %q", id, got, want)
		}
	}
	if slices.Contains(files.opened, "af_logo") || slices.Contains(files.opened, "af_huge") {
		t.Errorf("opened a file refused by its record: %v", files.opened)
	}
}

// One turn reads at most its budget of file content.
func TestAssistantFilesBoundEachTurn(t *testing.T) {
	files := newFakeFiles()
	chunk := strings.Repeat("y", maxReadableFileBytes)
	var ids []string
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		files.add(id, asstSpace, id+".txt", "text/plain", chunk)
		ids = append(ids, id)
	}
	_, read := fileTools(t, files, ids...)
	for _, id := range ids[:3] {
		if got := read(id); !strings.Contains(got, "yyyy") {
			t.Fatalf("read %s refused within budget: %q", id, got[:min(len(got), 80)])
		}
	}
	if got := read("a4"); !strings.Contains(got, "already read as much") {
		t.Errorf("read past budget = %q", got[:min(len(got), 80)])
	}
}

// The file tools exist only for an Assistant with readable files.
func TestAssistantFileToolsNeedAnAllowlist(t *testing.T) {
	if tools := newAssistantFileTools(newFakeFiles(), asstSpace, nil); tools != nil {
		t.Errorf("tools without an allowlist = %d", len(tools))
	}
	if tools := newAssistantFileTools(nil, asstSpace, []string{"af_policy"}); tools != nil {
		t.Errorf("tools without file storage = %d", len(tools))
	}
	f := newAssistantFixture()
	f.svc.Files = newFakeFiles()
	f.profile.ReadableFiles = []string{"af_policy"}
	f.turn(t, "what is the leave policy?")
	var names []string
	for _, d := range f.client.requests[0].Tools {
		names = append(names, d.Name)
	}
	if !slices.Contains(names, "ListFiles") || !slices.Contains(names, "ReadFile") {
		t.Errorf("tools = %v", names)
	}
}
