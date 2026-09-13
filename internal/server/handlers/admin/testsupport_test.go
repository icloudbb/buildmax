package admin

import (
	"github.com/icloudbb/buildmax/internal/testsupport"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// Fixtures this package's tests share. They are copies of the root package's
// rather than an import: a test helper shared across a package boundary makes
// the boundary softer than the code's, which is what splitting these packages
// was for.
const testSecret = "matrix-secret"

const matrixSpace = "tm_matrix"

// adminPostJSON drives one POST with a JSON body as the given user, for the
// handlers that take a request body rather than only a path.
func adminPostJSON(t *testing.T, mux *http.ServeMux, path, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	return adminRequestJSON(t, mux, http.MethodPost, path, userID, body)
}

// adminRequestJSON drives one request of any method with a JSON body as the
// given user, for the state sub-resources that read the target from the body.
func adminRequestJSON(t *testing.T, mux *http.ServeMux, method, path, userID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(userID, testSecret))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func adminRequestAs(t *testing.T, mux *http.ServeMux, c adminCase, userID string) *httptest.ResponseRecorder {
	t.Helper()
	path := regexp.MustCompile(`\{[^}]+\}`).ReplaceAllString(c.path, "nonexistent")
	req := httptest.NewRequest(c.method, path, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	if userID != "" {
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(userID, testSecret))
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}
