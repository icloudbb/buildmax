package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientCreateAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/users" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "new@corp.com" {
			t.Errorf("email = %q", body["email"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"u_9","email":"new@corp.com","has_password":false}`))
	}))
	defer srv.Close()

	account, err := NewClient(srv.URL).CreateAccount(context.Background(), "tok", "new@corp.com")
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}
	if account.ID != "u_9" || account.HasPassword {
		t.Fatalf("unexpected account: %+v", account)
	}
}

func TestClientIssueLoginCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/users/u_9/login-code" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"bmxlogin_abc","expires_at":"2026-09-06T01:00:00Z"}`))
	}))
	defer srv.Close()

	code, expiresAt, err := NewClient(srv.URL).IssueLoginCode(context.Background(), "tok", "u_9")
	if err != nil {
		t.Fatalf("IssueLoginCode: %v", err)
	}
	if code != "bmxlogin_abc" || expiresAt.IsZero() {
		t.Fatalf("unexpected code %q expires %v", code, expiresAt)
	}
}

func TestClientSetAccountDisabledReportsRevoked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/users/u_9/state" || r.Method != http.MethodPut {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u_9","email":"new@corp.com","disabled_at":"2026-09-06T00:00:00Z","has_password":true,"sessions_revoked":3}`))
	}))
	defer srv.Close()

	change, err := NewClient(srv.URL).SetAccountDisabled(context.Background(), "tok", "u_9", true)
	if err != nil {
		t.Fatalf("SetAccountDisabled: %v", err)
	}
	if !change.Disabled() || change.SessionsRevoked != 3 || len(change.CleanupFailed) != 0 {
		t.Fatalf("unexpected: %+v", change)
	}
}

func TestClientSetAccountDisabledReportsIncompleteCleanup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"u_9","email":"new@corp.com","disabled_at":"2026-09-06T00:00:00Z","sessions_revoked":1,"cleanup_failed":["schedules","runs"]}`))
	}))
	defer srv.Close()

	change, err := NewClient(srv.URL).SetAccountDisabled(context.Background(), "tok", "u_9", true)
	if err != nil {
		t.Fatalf("SetAccountDisabled: %v", err)
	}
	if !change.Disabled() || len(change.CleanupFailed) != 2 || change.CleanupFailed[1] != "runs" {
		t.Fatalf("unexpected: %+v", change)
	}
}

func TestClientListAccounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/users" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("q") != "corp" {
			t.Errorf("query not forwarded: %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users":[{"id":"u_9","email":"new@corp.com","has_password":false}],"total":1}`))
	}))
	defer srv.Close()

	accounts, total, err := NewClient(srv.URL).ListAccounts(context.Background(), "tok", "corp")
	if err != nil {
		t.Fatalf("ListAccounts: %v", err)
	}
	if len(accounts) != 1 || total != 1 || accounts[0].Email != "new@corp.com" {
		t.Fatalf("unexpected: %+v total=%d", accounts, total)
	}
}
