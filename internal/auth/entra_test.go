package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

const (
	testIssuer  = "https://login.microsoftonline.com/tenant-abc/v2.0"
	testClient  = "client-123"
	testSubject = "00000000-0000-0000-0000-000000000001"
)

var testPrivateKey = mustTestPrivateKey()

func mustTestPrivateKey() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
}

// idToken builds a signed RS256 JWT for the test JWKS endpoint.
func idToken(claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "test-key"})
	payload, _ := json.Marshal(claims)
	part := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(part))
	sig, err := rsa.SignPKCS1v15(rand.Reader, testPrivateKey, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return part + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func defaultClaims() map[string]any {
	return map[string]any{
		"sub":  testSubject,
		"iss":  testIssuer,
		"aud":  testClient,
		"exp":  time.Now().Add(time.Hour).Unix(),
		"upn":  "alice@corp.com",
		"name": "Alice",
	}
}

// providerWithToken wires a Provider whose token endpoint returns idToken.
func providerWithToken(t *testing.T, tok string) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/keys" {
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": "test-key",
				"n": base64.RawURLEncoding.EncodeToString(testPrivateKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString([]byte{1, 0, 1}),
			}}})
			return
		}
		fmt.Fprintf(w, `{"access_token":"a","token_type":"Bearer","expires_in":3600,"id_token":%q}`, tok)
	}))
	t.Cleanup(srv.Close)
	return &Provider{
		issuer:  testIssuer,
		jwksURL: srv.URL + "/keys",
		oauth: &oauth2.Config{
			ClientID:    testClient,
			RedirectURL: "https://app.example/api/auth/callback",
			Endpoint: oauth2.Endpoint{
				AuthURL:  "https://login.microsoftonline.com/tenant-abc/oauth2/v2.0/authorize",
				TokenURL: srv.URL + "/token",
			},
			Scopes: []string{"openid", "profile", "email"},
		},
	}
}

func TestNewProviderRequiresConfig(t *testing.T) {
	if _, err := NewProvider(Config{TenantID: "t", ClientID: ""}); err == nil {
		t.Fatal("expected error when client id missing")
	}
	if _, err := NewProvider(Config{TenantID: "", ClientID: "c"}); err == nil {
		t.Fatal("expected error when tenant id missing")
	}
	p, err := NewProvider(Config{TenantID: "tenant-abc", ClientID: testClient, RedirectURL: "https://app.example/cb"})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	if p.oauth.Endpoint.AuthURL != "https://login.microsoftonline.com/tenant-abc/oauth2/v2.0/authorize" {
		t.Fatalf("auth url = %q", p.oauth.Endpoint.AuthURL)
	}
	if p.oauth.Endpoint.TokenURL != "https://login.microsoftonline.com/tenant-abc/oauth2/v2.0/token" {
		t.Fatalf("token url = %q", p.oauth.Endpoint.TokenURL)
	}
}

func TestProviderLoginURL(t *testing.T) {
	p, err := NewProvider(Config{TenantID: "tenant-abc", ClientID: testClient, RedirectURL: "https://app.example/cb"})
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	raw := p.LoginURL("state-1", "verifier-1")
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != testClient || q.Get("state") != "state-1" {
		t.Fatalf("url params = %v", q)
	}
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		t.Fatalf("missing PKCE challenge: %v", q)
	}
	if q.Get("response_type") != "code" || q.Get("scope") == "" {
		t.Fatalf("missing code/scope: %v", q)
	}
}

func TestProviderExchangeValid(t *testing.T) {
	p := providerWithToken(t, idToken(defaultClaims()))
	id, err := p.Exchange(context.Background(), "code", "verifier")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if id.Subject != testSubject || id.Name != "Alice" || id.UPN != "alice@corp.com" {
		t.Fatalf("identity = %+v", id)
	}
}

func TestProviderExchangeAudienceArray(t *testing.T) {
	claims := defaultClaims()
	claims["aud"] = []string{"other-client", testClient}
	p := providerWithToken(t, idToken(claims))
	if _, err := p.Exchange(context.Background(), "code", "v"); err != nil {
		t.Fatalf("array audience should validate: %v", err)
	}
}

func TestProviderExchangeRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"wrong issuer", func(m map[string]any) { m["iss"] = "https://evil.example/v2.0" }},
		{"wrong audience", func(m map[string]any) { m["aud"] = "other-client" }},
		{"expired", func(m map[string]any) { m["exp"] = time.Now().Add(-time.Hour).Unix() }},
		{"missing subject", func(m map[string]any) { delete(m, "sub") }},
		{"malformed token", func(m map[string]any) {}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tok string
			if tc.name == "malformed token" {
				tok = "not-a-jwt"
			} else {
				claims := defaultClaims()
				tc.mutate(claims)
				tok = idToken(claims)
			}
			p := providerWithToken(t, tok)
			if _, err := p.Exchange(context.Background(), "code", "v"); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestMapRole(t *testing.T) {
	cases := []struct {
		name   string
		claims []string
		want   string
	}{
		{"empty", nil, ""},
		{"watcher", []string{"ACH.Watcher"}, "watcher"},
		{"processor", []string{"ACH.Processor"}, "processor"},
		{"admin", []string{"ACH.Admin"}, "admin"},
		{"super admin", []string{"ACH.SuperAdmin"}, "super_admin"},
		{"uri form", []string{"https://login.microsoftonline.com/t/app/ACH.Processor"}, "processor"},
		{"unrelated", []string{"User.Read", "SomeOther"}, ""},
		{"admin wins over others", []string{"ACH.Watcher", "ACH.Processor", "ACH.Admin"}, "admin"},
		{"super admin wins over admin", []string{"ACH.Admin", "ACH.SuperAdmin"}, "super_admin"},
		{"super admin with everything", []string{"ACH.Watcher", "ACH.Processor", "ACH.Admin", "ACH.SuperAdmin"}, "super_admin"},
		{"processor over watcher", []string{"ACH.Watcher", "ACH.Processor"}, "processor"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MapRole(tc.claims); got != tc.want {
				t.Fatalf("MapRole(%v) = %q, want %q", tc.claims, got, tc.want)
			}
		})
	}
}
