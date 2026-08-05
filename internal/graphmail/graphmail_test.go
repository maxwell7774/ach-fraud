package graphmail

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientTokenAndSend(t *testing.T) {
	var tokenCalls int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "client_credentials" {
			t.Errorf("grant_type = %q", r.Form.Get("grant_type"))
		}
		if r.Form.Get("scope") != "https://graph.microsoft.com/.default" {
			t.Errorf("scope = %q", r.Form.Get("scope"))
		}
		if r.Form.Get("client_secret") != "secret-1" {
			t.Errorf("client_secret not sent")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"token_type": "Bearer", "expires_in": 3600, "access_token": "tok-1"})
	}))
	defer tokenSrv.Close()

	var captured map[string]any
	graphSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer tok-1" {
			t.Errorf("authorization = %q", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/users/shared@corp.com/sendMail") {
			t.Errorf("path = %q", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&captured)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer graphSrv.Close()

	c := New(Config{ClientID: "cid", ClientSecret: "secret-1", TokenURL: tokenSrv.URL, GraphBaseURL: graphSrv.URL})
	ctx := context.Background()
	msg := Message{Subject: "Test alert", To: []string{"a@corp.com"}, Body: "hello"}
	if err := c.Send(ctx, "shared@corp.com", msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls = %d, want 1", tokenCalls)
	}

	// A second send reuses the cached token.
	if err := c.Send(ctx, "shared@corp.com", msg); err != nil {
		t.Fatalf("second send: %v", err)
	}
	if tokenCalls != 1 {
		t.Fatalf("token calls after second send = %d, want cached", tokenCalls)
	}

	inner, ok := captured["message"].(map[string]any)
	if !ok || inner["subject"] != "Test alert" {
		t.Fatalf("message = %+v", captured)
	}
	recips, ok := inner["toRecipients"].([]any)
	if !ok || len(recips) != 1 {
		t.Fatalf("recipients = %+v", inner["toRecipients"])
	}
}

func TestClientRefetchesTokenAfterExpiry(t *testing.T) {
	var tokenCalls int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		json.NewEncoder(w).Encode(map[string]any{"expires_in": 3600, "access_token": "tok"})
	}))
	defer tokenSrv.Close()
	graphSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer graphSrv.Close()

	c := New(Config{ClientID: "cid", TokenURL: tokenSrv.URL, GraphBaseURL: graphSrv.URL})
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return base }
	msg := Message{Subject: "x", To: []string{"a@b.c"}}
	ctx := context.Background()

	if err := c.Send(ctx, "s@x.com", msg); err != nil {
		t.Fatalf("send 1: %v", err)
	}
	// Advance past the cache window so the token is refetched.
	c.now = func() time.Time { return base.Add(2 * time.Hour) }
	if err := c.Send(ctx, "s@x.com", msg); err != nil {
		t.Fatalf("send 2: %v", err)
	}
	if tokenCalls != 2 {
		t.Fatalf("token calls = %d, want 2", tokenCalls)
	}
}

func TestClientSendError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/token"):
			json.NewEncoder(w).Encode(map[string]any{"expires_in": 3600, "access_token": "tok"})
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	c := New(Config{ClientID: "cid", TokenURL: srv.URL + "/token", GraphBaseURL: srv.URL})
	if err := c.Send(context.Background(), "s@x.com", Message{Subject: "x", To: []string{"a@b.c"}}); err == nil {
		t.Fatal("expected error on non-202")
	}
}
