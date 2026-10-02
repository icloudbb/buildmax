package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type strictBody struct {
	Name string `json:"name"`
}

func decodeWith(t *testing.T, decode func(http.ResponseWriter, *http.Request, any) bool, body string) (bool, *httptest.ResponseRecorder, strictBody) {
	t.Helper()
	var dst strictBody
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	return decode(rec, r, &dst), rec, dst
}

// A field the request type does not declare is refused and named: dropping it
// let a misspelled field produce a success for something never applied.
func TestDecodeJSONBodyRefusesAndNamesAnUnknownField(t *testing.T) {
	ok, rec, _ := decodeWith(t, DecodeJSONBody, `{"name":"a","secret_grants":[]}`)
	if ok || rec.Code != http.StatusBadRequest {
		t.Fatalf("ok=%v status=%d, want refused with 400", ok, rec.Code)
	}
	var body map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if !strings.Contains(body["error"], `unknown field "secret_grants"`) {
		t.Fatalf("error = %q, want it to name the field", body["error"])
	}
	if ok, _, dst := decodeWith(t, DecodeJSONBody, `{"name":"a"}`); !ok || dst.Name != "a" {
		t.Fatalf("a declared field: ok=%v dst=%+v", ok, dst)
	}
	if ok, rec, _ := decodeWith(t, DecodeJSONBody, ``); ok || rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty body on a route that needs one: ok=%v status=%d", ok, rec.Code)
	}
}

// An optional body may be absent, but one that is there is held to the same
// rule.
func TestDecodeOptionalJSONBodyAllowsOnlyAnEmptyBody(t *testing.T) {
	if ok, _, _ := decodeWith(t, DecodeOptionalJSONBody, ``); !ok {
		t.Fatal("an empty optional body was refused")
	}
	if ok, rec, _ := decodeWith(t, DecodeOptionalJSONBody, `{"refresh_tokn":"x"}`); ok || rec.Code != http.StatusBadRequest {
		t.Fatalf("a misspelled field in an optional body: ok=%v status=%d", ok, rec.Code)
	}
	if ok, rec, _ := decodeWith(t, DecodeOptionalJSONBody, `{not json`); ok || rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed optional body: ok=%v status=%d", ok, rec.Code)
	}
}
