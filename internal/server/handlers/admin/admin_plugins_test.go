package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	archive "github.com/icloudbb/buildmax/internal/infra/pluginarchive"
	"github.com/icloudbb/buildmax/internal/infra/pluginwire"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// pluginMux builds an admin handler whose catalog is real, over in-memory
// stores, so the routes are driven end to end rather than against a stub.
func pluginMux(t *testing.T) (*http.ServeMux, *mock.MockPluginStore, *mock.MockPluginPackageStorage) {
	t.Helper()
	catalog := mock.NewMockPluginStore()
	packages := mock.NewMockPluginPackageStorage()

	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	audits := &mock.MockAuditStore{}

	h := New(Config{
		JWTSecret: testSecret,
		Grants:    grants,
		Users:     users,
		Spaces:    &mock.MockSpaceStore{},
		Audits:    audits,
		Audit:     audit.NewRecorder(audits),
		Plugins: &pluginsvc.Service{
			Catalog:  catalog,
			Packages: packages,
			Audit:    audit.NewRecorder(audits),
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, catalog, packages
}

func pluginRequest(t *testing.T, mux *http.ServeMux, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(adminUser, testSecret))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func packageBytes(t *testing.T, manifest string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if _, err := archive.Pack(&buf, fstest.MapFS{
		"plugin.yaml":            &fstest.MapFile{Data: []byte(manifest)},
		"skills/review/SKILL.md": &fstest.MapFile{Data: []byte("# review\n")},
	}, archive.Limits{}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestAdminPluginPublishAndCatalog(t *testing.T) {
	mux, _, packages := pluginMux(t)
	data := packageBytes(t, "name: code-review\nversion: 1.2.0\ndescription: Reviews.\n")

	// The body is the archive itself; the claim about where it came from
	// travels beside it.
	rec := pluginRequest(t, mux, "POST",
		"/api/admin/plugins/code-review/releases?source_remote=git@example.com:x.git&source_commit=abc123&source_dirty=true",
		data)
	if rec.Code != http.StatusCreated {
		t.Fatalf("publish = %d: %s", rec.Code, rec.Body)
	}
	var release coreplugin.Release
	if err := json.Unmarshal(rec.Body.Bytes(), &release); err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.2.0" || release.Digest == "" {
		t.Errorf("release = %+v", release)
	}
	if !release.Source.Dirty || release.Source.Commit != "abc123" {
		t.Errorf("source claim = %+v", release.Source)
	}
	if len(packages.Objects) != 1 {
		t.Errorf("stored %d objects, want 1", len(packages.Objects))
	}

	// A first publish created the entry, and the admin listing shows it.
	rec = pluginRequest(t, mux, "GET", "/api/admin/plugins", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d: %s", rec.Code, rec.Body)
	}
	var list pluginwire.CatalogResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Plugins) != 1 || list.Plugins[0].Name != "code-review" {
		t.Errorf("catalog = %+v", list.Plugins)
	}

	rec = pluginRequest(t, mux, "GET", "/api/admin/plugins/code-review/releases", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("releases = %d: %s", rec.Code, rec.Body)
	}
}

func TestAdminPluginPublishRefusals(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		manifest string
		want     int
	}{
		{"names another plugin", "/api/admin/plugins/code-review/releases",
			"name: something-else\nversion: 1.0.0\n", http.StatusBadRequest},
		{"no version", "/api/admin/plugins/code-review/releases",
			"name: code-review\n", http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mux, _, packages := pluginMux(t)
			rec := pluginRequest(t, mux, "POST", tc.path, packageBytes(t, tc.manifest))
			if rec.Code != tc.want {
				t.Errorf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body)
			}
			if len(packages.Objects) != 0 {
				t.Error("a refused publish stored bytes")
			}
		})
	}
}

// The same request that succeeded a moment ago is a conflict, not a bad
// request: nothing about it is malformed.
func TestAdminPluginRepublishConflicts(t *testing.T) {
	mux, _, _ := pluginMux(t)
	data := packageBytes(t, "name: code-review\nversion: 1.2.0\n")

	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins/code-review/releases", data); rec.Code != http.StatusCreated {
		t.Fatalf("first publish = %d: %s", rec.Code, rec.Body)
	}
	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins/code-review/releases", data); rec.Code != http.StatusConflict {
		t.Errorf("republish = %d, want 409: %s", rec.Code, rec.Body)
	}
}

func TestAdminPluginArchiveAndYank(t *testing.T) {
	mux, _, _ := pluginMux(t)
	data := packageBytes(t, "name: code-review\nversion: 1.0.0\n")
	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins/code-review/releases", data); rec.Code != http.StatusCreated {
		t.Fatalf("publish = %d: %s", rec.Code, rec.Body)
	}

	// A withdrawal names no reason and is still a withdrawal.
	if rec := pluginRequest(t, mux, "PUT", "/api/admin/plugins/code-review/releases/1.0.0/state", []byte(`{"yanked":true}`)); rec.Code != http.StatusNoContent {
		t.Errorf("yank = %d: %s", rec.Code, rec.Body)
	}
	// Un-yanking is not a capability the catalog has, so the state route refuses it.
	if rec := pluginRequest(t, mux, "PUT", "/api/admin/plugins/code-review/releases/1.0.0/state", []byte(`{"yanked":false}`)); rec.Code != http.StatusBadRequest {
		t.Errorf("un-yank = %d, want 400", rec.Code)
	}
	if rec := pluginRequest(t, mux, "PUT", "/api/admin/plugins/code-review/releases/9.9.9/state", []byte(`{"yanked":true}`)); rec.Code != http.StatusNotFound {
		t.Errorf("yanking a missing release = %d, want 404", rec.Code)
	}

	if rec := pluginRequest(t, mux, "PUT", "/api/admin/plugins/code-review/state", []byte(`{"archived":true}`)); rec.Code != http.StatusNoContent {
		t.Errorf("archive = %d: %s", rec.Code, rec.Body)
	}
	// Archiving refuses new releases; it deletes nothing.
	next := packageBytes(t, "name: code-review\nversion: 1.1.0\n")
	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins/code-review/releases", next); rec.Code != http.StatusConflict {
		t.Errorf("publishing to an archived entry = %d, want 409", rec.Code)
	}
	if rec := pluginRequest(t, mux, "PUT", "/api/admin/plugins/code-review/state", []byte(`{"archived":false}`)); rec.Code != http.StatusNoContent {
		t.Errorf("unarchive = %d: %s", rec.Code, rec.Body)
	}
	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins/code-review/releases", next); rec.Code != http.StatusCreated {
		t.Errorf("publishing after restore = %d: %s", rec.Code, rec.Body)
	}
	if rec := pluginRequest(t, mux, "PUT", "/api/admin/plugins/absent/state", []byte(`{"archived":true}`)); rec.Code != http.StatusNotFound {
		t.Errorf("archiving a missing entry = %d, want 404", rec.Code)
	}
}

func TestAdminPluginCreateEntry(t *testing.T) {
	mux, _, _ := pluginMux(t)
	body, err := json.Marshal(pluginwire.CreatePluginRequest{Name: "code-review", DisplayName: "Code Review"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins", body); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d: %s", rec.Code, rec.Body)
	}
	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins", body); rec.Code != http.StatusConflict {
		t.Errorf("duplicate name = %d, want 409", rec.Code)
	}

	bad, err := json.Marshal(pluginwire.CreatePluginRequest{Name: "Bad Name"})
	if err != nil {
		t.Fatal(err)
	}
	if rec := pluginRequest(t, mux, "POST", "/api/admin/plugins", bad); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid name = %d, want 400", rec.Code)
	}
}

// A deployment with no Marketplace says so rather than serving an empty
// catalog, and must not dereference a service it does not have.
func TestAdminPluginRoutesWithoutAMarketplace(t *testing.T) {
	mux, _ := adminMux(t)
	for _, path := range []string{"/api/admin/plugins", "/api/admin/plugins/code-review/releases"} {
		rec := pluginRequest(t, mux, "GET", path, nil)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("GET %s = %d, want 503", path, rec.Code)
		}
	}
}
