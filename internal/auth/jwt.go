package auth

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
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

type jwtHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
}

type jwksDocument struct {
	Keys []jwk `json:"keys"`
}

type jwk struct {
	KeyType   string `json:"kty"`
	Use       string `json:"use"`
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Modulus   string `json:"n"`
	Exponent  string `json:"e"`
}

const jwksCacheTTL = time.Hour

// parseIDToken verifies the RS256 signature against Entra's cached JWKS and
// then validates the claims we rely on: issuer, audience, expiry, and subject.
func (p *Provider) parseIDToken(ctx context.Context, raw, issuer, clientID string, now time.Time) (jwtClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return jwtClaims{}, errors.New("id_token is not a JWT")
	}
	var header jwtHeader
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(headerBytes, &header) != nil {
		return jwtClaims{}, errors.New("malformed id_token header")
	}
	if header.Algorithm != "RS256" || header.KeyID == "" {
		return jwtClaims{}, errors.New("id_token has unsupported signing algorithm or missing key id")
	}
	keys, err := p.signingKeys(ctx, header.KeyID)
	if err != nil {
		return jwtClaims{}, fmt.Errorf("loading id_token signing keys: %w", err)
	}
	key := keys[header.KeyID]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return jwtClaims{}, errors.New("malformed id_token signature")
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], signature); err != nil {
		return jwtClaims{}, errors.New("invalid id_token signature")
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

func (p *Provider) signingKeys(ctx context.Context, wanted string) (map[string]*rsa.PublicKey, error) {
	p.jwksMu.Lock()
	defer p.jwksMu.Unlock()
	if p.keys != nil && time.Since(p.fetched) < jwksCacheTTL {
		if _, ok := p.keys[wanted]; ok {
			return p.keys, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.jwksURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned %s", resp.Status)
	}
	var doc jwksDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.KeyType != "RSA" || (k.Use != "" && k.Use != "sig") || (k.Algorithm != "" && k.Algorithm != "RS256") || k.KeyID == "" {
			continue
		}
		n, err := base64.RawURLEncoding.DecodeString(k.Modulus)
		if err != nil {
			continue
		}
		e, err := base64.RawURLEncoding.DecodeString(k.Exponent)
		if err != nil || len(e) == 0 || len(n) == 0 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		keys[k.KeyID] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
	}
	if len(keys) == 0 {
		return nil, errors.New("JWKS contained no usable RSA signing keys")
	}
	p.keys = keys
	p.fetched = time.Now()
	if _, ok := keys[wanted]; !ok {
		return nil, fmt.Errorf("JWKS has no key %q", wanted)
	}
	return keys, nil
}
