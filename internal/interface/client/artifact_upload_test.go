package client

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestPublishArtifactUploadsAndReturnsID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("hello artifact"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	var gotQuery url.Values
	var gotFilename, gotContent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/artifacts" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotQuery = r.URL.Query()
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Errorf("file part: %v", err)
		} else {
			defer func() { _ = f.Close() }()
			gotFilename = hdr.Filename
			buf := make([]byte, 64)
			n, _ := f.Read(buf)
			gotContent = string(buf[:n])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"gsyt7at6cjfr33d73mta","filename":"out.txt","size_bytes":14,"share":{"url":"http://x/shared/artifacts/tok/meta"}}`))
	}))
	defer srv.Close()

	art, err := NewClient(srv.URL).PublishArtifact(t.Context(), "tok", "tm_1", "My File", path, "out.txt", true)
	if err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if gotQuery.Get("space_id") != "tm_1" || gotQuery.Get("title") != "My File" || gotQuery.Get("share") != "1" {
		t.Fatalf("query = %v", gotQuery)
	}
	if gotFilename != "out.txt" || gotContent != "hello artifact" {
		t.Fatalf("uploaded file = %q / %q", gotFilename, gotContent)
	}
	if art.ID != "gsyt7at6cjfr33d73mta" || art.SizeBytes != 14 || art.ShareURL == "" {
		t.Fatalf("artifact = %+v", art)
	}
}

// Without --space the request carries no space_id, so the server files it in the
// caller's personal space.
func TestPublishArtifactOmitsEmptyQuery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	var raw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"id1","filename":"a.bin","size_bytes":1}`))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL).PublishArtifact(t.Context(), "tok", "", "", path, "a.bin", false); err != nil {
		t.Fatalf("PublishArtifact: %v", err)
	}
	if raw != "" {
		t.Fatalf("query = %q, want empty when no space/title/share given", raw)
	}
}
