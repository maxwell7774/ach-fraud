package auth

import (
	"context"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
)

func newManager(t *testing.T) (*SessionManager, *fakeports.Store, domain.User) {
	t.Helper()
	st := fakeports.NewStore()
	ctx := context.Background()
	u, err := st.UpsertUser(ctx, domain.User{Subject: "sub-1", Name: "Alice"})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	return &SessionManager{Store: st, TTL: time.Hour}, st, u
}

func TestSessionCreateVerifyDestroy(t *testing.T) {
	m, _, u := newManager(t)
	ctx := context.Background()

	tok, err := m.Create(ctx, u.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if tok == "" {
		t.Fatal("empty token")
	}

	got, err := m.Verify(ctx, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.ID != u.ID {
		t.Fatalf("verified user = %s, want %s", got.ID, u.ID)
	}

	if err := m.Destroy(ctx, tok); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, err := m.Verify(ctx, tok); err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after destroy, got %v", err)
	}
}

func TestSessionVerifyUnknown(t *testing.T) {
	m, _, _ := newManager(t)
	if _, err := m.Verify(context.Background(), "bogus"); err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound for bogus token, got %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	st := fakeports.NewStore()
	ctx := context.Background()
	u, _ := st.UpsertUser(ctx, domain.User{Subject: "sub-2"})

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	m := &SessionManager{Store: st, TTL: time.Hour, Now: func() time.Time { return base }}

	tok, err := m.Create(ctx, u.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Still valid one minute before expiry.
	if _, err := m.Verify(ctx, tok); err != nil {
		t.Fatalf("verify before expiry: %v", err)
	}
	// Expired after TTL elapses; the row is lazily removed.
	m.Now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, err := m.Verify(ctx, tok); err != domain.ErrNotFound {
		t.Fatalf("expected ErrNotFound after expiry, got %v", err)
	}
	if _, err := st.GetSessionByTokenHash(ctx, HashToken(tok)); err != domain.ErrNotFound {
		t.Fatalf("expired session not cleaned up: %v", err)
	}
}

func TestHashTokenStable(t *testing.T) {
	if HashToken("abc") != HashToken("abc") {
		t.Fatal("hash not deterministic")
	}
	if HashToken("abc") == HashToken("abd") {
		t.Fatal("distinct tokens collided")
	}
}

func TestNewTokenUnique(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	b, err := NewToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if a == b {
		t.Fatal("tokens collided")
	}
}
