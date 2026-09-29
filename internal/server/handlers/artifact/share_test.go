package artifact

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/mock"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
)

func (f *fixture) createShare(t *testing.T, userID, artifactID string) shareResponse {
	t.Helper()
	rec := f.do(t, http.MethodPost, "/api/artifacts/"+artifactID+"/shares", userID, nil, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create share status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out shareResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode share response: %v", err)
	}
	return out
}

func TestCreateShareReturnsAPublicLink(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "report.md", "# hello")
	share := f.createShare(t, userMember, created.ID)

	if share.Token == "" {
		t.Fatal("share response carried no token")
	}
	wantURL := testPublicBaseURL + "/shared/artifacts/" + share.Token
	if share.URL != wantURL {
		t.Errorf("url = %q, want %q", share.URL, wantURL)
	}
	if !strings.HasSuffix(share.DownloadURL, "/raw?dl=1") {
		t.Errorf("download_url = %q, want it to end with /raw?dl=1", share.DownloadURL)
	}
}

// A later read of the share never carries the token again: only creation does.
func TestListSharesNeverCarriesTheToken(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "report.md", "# hello")
	share := f.createShare(t, userOwner, created.ID)

	rec := f.do(t, http.MethodGet, "/api/artifacts/"+created.ID+"/shares", userOwner, nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, share.Token) {
		t.Error("the share listing leaked the token")
	}
	if !strings.Contains(body, share.ShareID) {
		t.Errorf("the listing did not include the share id %q", share.ShareID)
	}
}

func TestSharedContentAndMetaByToken(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "report.md", "# hello")
	share := f.createShare(t, userOwner, created.ID)

	// No Authorization header: the token is the whole authorization.
	content := f.do(t, http.MethodGet, "/shared/artifacts/"+share.Token+"/raw", "", nil, "")
	if content.Code != http.StatusOK {
		t.Fatalf("content status = %d, want 200: %s", content.Code, content.Body.String())
	}
	if content.Body.String() != "# hello" {
		t.Errorf("content = %q, want the uploaded bytes", content.Body.String())
	}

	meta := f.do(t, http.MethodGet, "/shared/artifacts/"+share.Token+"/meta", "", nil, "")
	if meta.Code != http.StatusOK {
		t.Fatalf("meta status = %d, want 200", meta.Code)
	}
	var m sharedMetaResponse
	if err := json.Unmarshal(meta.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode meta: %v", err)
	}
	if m.Filename != "report.md" || m.Preview != "inline" {
		t.Errorf("meta = %+v, want filename report.md and preview inline", m)
	}
}

// A shared HTML artifact reaches an anonymous viewer under the same
// opaque-origin sandbox the authenticated route uses.
func TestSharedHTMLCarriesTheSandboxCSP(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "proto.html", "<h1>hi</h1>")
	share := f.createShare(t, userOwner, created.ID)

	rec := f.do(t, http.MethodGet, "/shared/artifacts/"+share.Token+"/raw", "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	csp := rec.Header().Get("Content-Security-Policy")
	if !strings.HasPrefix(csp, "sandbox") || strings.Contains(csp, "allow-same-origin") {
		t.Errorf("CSP = %q, want a sandbox policy without allow-same-origin", csp)
	}
}

func TestUnknownTokenIsNotFound(t *testing.T) {
	f := newFixture(t)
	for _, path := range []string{"/shared/artifacts/share_bogus/raw", "/shared/artifacts/share_bogus/meta"} {
		if code := f.do(t, http.MethodGet, path, "", nil, "").Code; code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", path, code)
		}
	}
}

func TestRevokedShareResolvesToNotFound(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "report.md", "# hello")
	share := f.createShare(t, userOwner, created.ID)

	del := f.do(t, http.MethodDelete, "/api/artifacts/"+created.ID+"/shares/"+share.ShareID, userOwner, nil, "")
	if del.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, want 204: %s", del.Code, del.Body.String())
	}
	if code := f.do(t, http.MethodGet, "/shared/artifacts/"+share.Token+"/raw", "", nil, "").Code; code != http.StatusNotFound {
		t.Errorf("revoked token content = %d, want 404", code)
	}
}

// Deleting the artifact makes every link to it resolve to 404 with no separate
// revoke step.
func TestDeletedArtifactMakesShareNotFound(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "report.md", "# hello")
	share := f.createShare(t, userOwner, created.ID)

	if code := f.do(t, http.MethodDelete, "/api/artifacts/"+created.ID, userOwner, nil, "").Code; code != http.StatusNoContent {
		t.Fatalf("delete artifact status = %d, want 204", code)
	}
	if code := f.do(t, http.MethodGet, "/shared/artifacts/"+share.Token+"/raw", "", nil, "").Code; code != http.StatusNotFound {
		t.Errorf("share to deleted artifact = %d, want 404", code)
	}
}

// A member may revoke a link they created; a colleague's link is not theirs to
// withdraw, but an owner may withdraw any.
func TestRevokeSharePolicy(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "report.md", "# hello")
	memberShare := f.createShare(t, userMember, created.ID)

	// A different member cannot revoke the member's share.
	//nolint:noctx
	if code := f.do(t, http.MethodDelete, "/api/artifacts/"+created.ID+"/shares/"+memberShare.ShareID, userOther, nil, "").Code; code == http.StatusNoContent {
		t.Error("a non-member revoked a share they did not create")
	}
	// The owner can.
	if code := f.do(t, http.MethodDelete, "/api/artifacts/"+created.ID+"/shares/"+memberShare.ShareID, userOwner, nil, "").Code; code != http.StatusNoContent {
		t.Errorf("owner revoke = %d, want 204", code)
	}
}

func TestUploadWithShareFlagReturnsALink(t *testing.T) {
	f := newFixture(t)
	body, contentType := multipartBody(t, "report.md", "# hi")
	rec := f.do(t, http.MethodPost, "/api/spaces/"+spaceA+"/artifacts?share=1", userOwner, body, contentType)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	var out artifactResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Share == nil || out.Share.Token == "" || out.Share.URL == "" {
		t.Fatalf("upload with share=1 returned no link: %+v", out)
	}
}

// Sharing off: create is refused, and an upload that asked for a link still
// publishes the artifact and reports the reason rather than failing.
func TestSharingUnconfigured(t *testing.T) {
	f := newFixture(t)
	// Rebuild the service with no public base URL, sharing off.
	f.svc.PublicBaseURL = ""

	created := f.upload(t, userOwner, spaceA, "report.md", "# hi")
	if code := f.do(t, http.MethodPost, "/api/artifacts/"+created.ID+"/shares", userOwner, nil, "").Code; code != http.StatusServiceUnavailable {
		t.Errorf("create share with no base URL = %d, want 503", code)
	}

	body, contentType := multipartBody(t, "again.md", "# hi")
	rec := f.do(t, http.MethodPost, "/api/spaces/"+spaceA+"/artifacts?share=1", userOwner, body, contentType)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201 even with sharing off", rec.Code)
	}
	var out artifactResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.Share != nil {
		t.Error("a link was returned though sharing is off")
	}
	if out.ShareError == "" {
		t.Error("no share_error reported for a share requested with sharing off")
	}
}

// An upload names the artifact's Portal page on the public origin, and names
// no page at all without one: a client relays this address rather than
// rendering one from however it reached the server.
func TestUploadNamesThePortalPageOnlyOnAPublicOrigin(t *testing.T) {
	f := newFixture(t)
	created := f.upload(t, userOwner, spaceA, "report.md", "# hi")
	if want := testPublicBaseURL + "/#/artifact/" + created.ID; created.URL != want {
		t.Errorf("url = %q, want %q", created.URL, want)
	}

	f.svc.PublicBaseURL = ""
	bare := f.upload(t, userOwner, spaceA, "again.md", "# hi")
	if bare.URL != "" {
		t.Errorf("url = %q with no public origin, want none", bare.URL)
	}
}

// A deployment with the artifact capability but no share store cannot share.
func TestSharesAvailableRequiresBothHalves(t *testing.T) {
	store := &mock.MockArtifactStore{}
	svc := &artifactsvc.Service{Artifacts: store, Storage: mock.NewMockArtifactStorage()}
	if svc.SharesAvailable() {
		t.Error("SharesAvailable with no share store and no base URL")
	}
	svc.PublicBaseURL = "https://x"
	if svc.SharesAvailable() {
		t.Error("SharesAvailable with a base URL but no share store")
	}
	svc.Shares = mock.NewMockArtifactShareStore(store)
	if !svc.SharesAvailable() {
		t.Error("SharesAvailable false with both halves present")
	}
}
