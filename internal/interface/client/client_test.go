package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRequestOTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/otp" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["email"] != "a@b.com" || body["intent"] != "login" {
			t.Errorf("unexpected body: %v", body)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message":"account_created"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	if err := c.RequestOTP(context.Background(), "a@b.com", "login"); err != nil {
		t.Fatalf("RequestOTP: %v", err)
	}
}

func TestClientRequestOTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"user not found"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	err := c.RequestOTP(context.Background(), "a@b.com", "login")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "server 404: user not found" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestClientLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/auth/login" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["platform"] != "cli" {
			t.Errorf("expected platform=cli, got %q", body["platform"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"jwt123","user":{"id":"u_1","email":"a@b.com","name":"Alice"}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	lr, err := c.Login(context.Background(), "a@b.com", "123456", "cli")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if lr.Token != "jwt123" || lr.User.Email != "a@b.com" || lr.User.ID != "u_1" {
		t.Fatalf("unexpected response: %+v", lr)
	}
}

func TestClientLoginError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid otp"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Login(context.Background(), "a@b.com", "wrong", "cli")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "server 401: invalid otp" {
		t.Fatalf("unexpected error: %v", err)
	}
}
