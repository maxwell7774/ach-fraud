package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

const (
	// SessionCookieName carries the raw session token.
	SessionCookieName = "ach_session"
	// OAuthCookieName carries the short-lived OAuth state + PKCE verifier
	// between the login redirect and the callback.
	OAuthCookieName = "ach_oauth"
)

// SessionManager issues and verifies opaque session cookies. Only the sha256
// of a token is persisted; the raw token travels in the cookie.
type SessionManager struct {
	Store ports.Store
	TTL   time.Duration
	Now   func() time.Time
}

// NewToken returns a random raw session token (base64url, 32 random bytes).
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the hex sha256 of a raw token.
func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return fmt.Sprintf("%x", sum)
}

func (m *SessionManager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Create stores a new session for the user and returns the raw token to set in
// the cookie.
func (m *SessionManager) Create(ctx context.Context, userID uuid.UUID) (string, error) {
	tok, err := NewToken()
	if err != nil {
		return "", err
	}
	if _, err := m.Store.CreateSession(ctx, domain.Session{
		UserID:    userID,
		TokenHash: HashToken(tok),
		ExpiresAt: m.now().Add(m.TTL),
	}); err != nil {
		return "", err
	}
	return tok, nil
}

// Verify resolves a raw token to its user. An unknown or expired token returns
// an error; expired sessions are deleted lazily.
func (m *SessionManager) Verify(ctx context.Context, token string) (domain.User, error) {
	sess, err := m.Store.GetSessionByTokenHash(ctx, HashToken(token))
	if err != nil {
		return domain.User{}, err
	}
	if m.now().After(sess.ExpiresAt) {
		_ = m.Store.DeleteSession(ctx, sess.ID)
		return domain.User{}, domain.ErrNotFound
	}
	return m.Store.GetUserByID(ctx, sess.UserID)
}

// Destroy invalidates the session for a raw token.
func (m *SessionManager) Destroy(ctx context.Context, token string) error {
	sess, err := m.Store.GetSessionByTokenHash(ctx, HashToken(token))
	if err != nil {
		return err
	}
	return m.Store.DeleteSession(ctx, sess.ID)
}
