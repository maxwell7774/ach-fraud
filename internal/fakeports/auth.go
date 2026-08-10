package fakeports

import (
	"context"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) UpsertUser(ctx context.Context, in domain.User) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, u := range s.users {
		if u.Subject == in.Subject {
			u.UPN = in.UPN
			u.Email = in.Email
			u.Name = in.Name
			if in.Role != "" {
				u.Role = in.Role
			}
			u.LastLogin = time.Now()
			s.users[u.ID] = u
			return u, nil
		}
	}
	in.ID = uuid.New()
	in.LastLogin = time.Now()
	in.CreatedAt = time.Now()
	if in.Role == "" {
		in.Role = domain.RoleWatcher
	}
	s.users[in.ID] = in
	return in, nil
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (s *Store) CreateSession(ctx context.Context, in domain.Session) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.ID = uuid.New()
	in.CreatedAt = time.Now()
	s.sess[in.ID] = in
	return in, nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, sess := range s.sess {
		if sess.TokenHash == tokenHash {
			return sess, nil
		}
	}
	return domain.Session{}, domain.ErrNotFound
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sess[id]; !ok {
		return domain.ErrNotFound
	}
	delete(s.sess, id)
	return nil
}
