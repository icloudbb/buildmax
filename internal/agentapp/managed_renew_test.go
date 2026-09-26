package agentapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A worker's run token cannot be renewed, so its client is left exactly as the
// worker built it — trust and all.
func TestRenewingManagedClientLeavesAnUnrenewableCredentialAlone(t *testing.T) {
	base := &http.Client{Timeout: time.Minute}
	if got := renewingManagedClient(base, "https://buildmax.example.com", nil); got != base {
		t.Error("a client without a renewal was replaced")
	}
}

// A signed-in surface's managed call survives the server refusing a token
// that still looks valid, and the caller's own client settings carry over.
func TestRenewingManagedClientRetriesARefusedCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	base := &http.Client{Timeout: time.Minute}
	var renewedFor string
	c := renewingManagedClient(base, srv.URL, func(serverURL, rejected string) (string, error) {
		renewedFor = serverURL + " " + rejected
		return "fresh", nil
	})
	if c.Timeout != time.Minute {
		t.Error("the caller's client settings were dropped")
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL, nil)
	req.Header.Set("Authorization", "Bearer stale")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want the retry's 200", resp.StatusCode)
	}
	if renewedFor != srv.URL+" stale" {
		t.Errorf("renewal asked for %q, want this server and the refused token", renewedFor)
	}
}
