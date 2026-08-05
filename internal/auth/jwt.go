package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// clockSkew tolerates a small difference between the server clock and the
// id_token's exp claim.
const clockSkew = 60 * time.Second

// jwtClaims is the subset of the id_token claims we read.
type jwtClaims struct {
	Subject           string          `json:"sub"`
	Issuer            string          `json:"iss"`
	Audience          json.RawMessage `json:"aud"`
	Expires           int64           `json:"exp"`
	UPN               string          `json:"upn"`
	Email             string          `json:"email"`
	Name              string          `json:"name"`
	PreferredUsername string          `json:"preferred_username"`
	Roles             []string        `json:"roles"`
}

// hasAudience reports whether the audience claim (string or array) contains
// the expected client id.
func (c jwtClaims) hasAudience(clientID string) bool {
	var one string
	if err := json.Unmarshal(c.Audience, &one); err == nil {
		return one == clientID
	}
	var many []string
	if err := json.Unmarshal(c.Audience, &many); err == nil {
		for _, a := range many {
			if a == clientID {
				return true
			}
		}
	}
	return false
}

// parseIDToken decodes the id_token payload and validates the claims we rely
// on: issuer, audience, expiry, and a present subject. In the server-to-server
// authorization-code + PKCE exchange the token arrives over TLS directly from
// Entra, so no signature verification is required; these checks still reject a
// token minted for a different tenant, client, or an expired one.
func parseIDToken(raw string, issuer, clientID string, now time.Time) (jwtClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return jwtClaims{}, errors.New("id_token is not a JWT")
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, errors.New("malformed id_token payload")
	}
	var claims jwtClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return jwtClaims{}, err
	}
	if claims.Subject == "" {
		return jwtClaims{}, errors.New("id_token missing subject")
	}
	if now.Add(clockSkew).Unix() >= claims.Expires {
		return jwtClaims{}, errors.New("id_token expired")
	}
	if claims.Issuer != issuer {
		return jwtClaims{}, fmt.Errorf("id_token issuer %q, want %q", claims.Issuer, issuer)
	}
	if !claims.hasAudience(clientID) {
		return jwtClaims{}, errors.New("id_token audience mismatch")
	}
	return claims, nil
}
