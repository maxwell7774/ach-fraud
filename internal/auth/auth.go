// Package auth implements the Microsoft Entra (OAuth2/OIDC) login and the
// server-side sessions that back the web API. The Provider drives the
// authorization-code + PKCE flow and verifies id_token signatures and claims
// using Entra's cached JWKS and the standard library;
// the SessionManager issues and verifies opaque session cookies whose sha256
// lives in the store.
package auth

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/27actions/ach/internal/domain"

	"golang.org/x/oauth2"
)

// Config configures the Entra OIDC provider.
type Config struct {
	TenantID     string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

// Issuer returns the tenant's OIDC issuer URL.
func (c Config) Issuer() string {
	if c.TenantID == "" {
		return ""
	}
	return "https://login.microsoftonline.com/" + c.TenantID + "/v2.0"
}

// Identity is the subset of the id_token claims we persist.
type Identity struct {
	Subject string // Entra object id, stable across renames
	UPN     string
	Email   string
	Name    string
	Roles   []string // Entra app-role claim values
}

// Entra app-role values. Users are assigned these in the Entra app
// registration; the id_token carries them in the "roles" claim.
const (
	entraRoleSuperAdmin = "ACH.SuperAdmin"
	entraRoleAdmin      = "ACH.Admin"
	entraRoleProcessor  = "ACH.Processor"
	entraRoleWatcher    = "ACH.Watcher"
)

// MapRole maps Entra app-role claim values to the canonical domain role,
// honoring SuperAdmin > Admin > Processor > Watcher precedence. It returns ""
// when no recognized role is present.
func MapRole(claims []string) string {
	role := ""
	for _, c := range claims {
		switch {
		case matchesRole(c, entraRoleSuperAdmin):
			return domain.RoleSuperAdmin
		case matchesRole(c, entraRoleAdmin):
			role = domain.RoleAdmin
		case matchesRole(c, entraRoleProcessor):
			if role != domain.RoleAdmin {
				role = domain.RoleProcessor
			}
		case matchesRole(c, entraRoleWatcher):
			if role == "" {
				role = domain.RoleWatcher
			}
		}
	}
	return role
}

// matchesRole matches an Entra claim value, accepting either the bare value
// or the URI form (…/ACH.Admin).
func matchesRole(claim, name string) bool {
	return claim == name || strings.HasSuffix(claim, "/"+name)
}

// Entra is the login flow the HTTP API depends on, so tests can fake it.
type Entra interface {
	LoginURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (Identity, error)
}

// Provider implements Entra. Endpoints are the fixed Entra v2.0 URLs for the
// tenant; we never act on behalf of the user, so no resource/Graph scopes or
// delegated permissions are involved.
type Provider struct {
	issuer  string
	oauth   *oauth2.Config
	jwksURL string
	jwksMu  sync.Mutex
	keys    map[string]*rsa.PublicKey
	fetched time.Time
}

var _ Entra = (*Provider)(nil)

// NewProvider builds the token client against the tenant's fixed endpoints.
func NewProvider(cfg Config) (*Provider, error) {
	if cfg.TenantID == "" || cfg.ClientID == "" {
		return nil, errors.New("tenant id and client id are required")
	}
	base := "https://login.microsoftonline.com/" + cfg.TenantID + "/oauth2/v2.0"
	return &Provider{
		issuer:  cfg.Issuer(),
		jwksURL: "https://login.microsoftonline.com/" + cfg.TenantID + "/discovery/v2.0/keys",
		oauth: &oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint: oauth2.Endpoint{
				AuthURL:  base + "/authorize",
				TokenURL: base + "/token",
			},
			Scopes: []string{"openid", "profile", "email"},
		},
	}, nil
}

// LoginURL builds the tenant authorize URL carrying the state and a PKCE
// S256 challenge for the verifier.
func (p *Provider) LoginURL(state, verifier string) string {
	return p.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))
}

// Exchange trades the callback code for tokens and returns the verified
// identity from the id_token.
func (p *Provider) Exchange(ctx context.Context, code, verifier string) (Identity, error) {
	tok, err := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return Identity{}, fmt.Errorf("exchanging code: %w", err)
	}
	rawIDToken, ok := tok.Extra("id_token").(string)
	if !ok {
		return Identity{}, errors.New("token response contained no id_token")
	}
	claims, err := p.parseIDToken(ctx, rawIDToken, p.issuer, p.oauth.ClientID, time.Now())
	if err != nil {
		return Identity{}, fmt.Errorf("validating id_token: %w", err)
	}
	upn := claims.UPN
	if upn == "" {
		upn = claims.PreferredUsername
	}
	return Identity{
		Subject: claims.Subject,
		UPN:     upn,
		Email:   claims.Email,
		Name:    claims.Name,
		Roles:   claims.Roles,
	}, nil
}
