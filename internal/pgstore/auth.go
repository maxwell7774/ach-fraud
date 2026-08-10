package pgstore

import (
	"context"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) UpsertUser(ctx context.Context, in domain.User) (domain.User, error) {
	row, err := s.q.UpsertUser(ctx, UpsertUserParams{
		Subject: in.Subject,
		Upn:     in.UPN,
		Email:   in.Email,
		Name:    in.Name,
		Column5: in.Role,
	})
	if err != nil {
		return domain.User{}, err
	}
	return toDomainUser(row), nil
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := s.q.GetUserByID(ctx, toPgUUID(id))
	if err != nil {
		return domain.User{}, translate(err)
	}
	return toDomainUser(row), nil
}

func (s *Store) CreateSession(ctx context.Context, in domain.Session) (domain.Session, error) {
	row, err := s.q.CreateSession(ctx, CreateSessionParams{
		UserID:    toPgUUID(in.UserID),
		TokenHash: in.TokenHash,
		ExpiresAt: toPgTime(in.ExpiresAt),
	})
	if err != nil {
		return domain.Session{}, err
	}
	return toDomainSession(row), nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	row, err := s.q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return domain.Session{}, translate(err)
	}
	return toDomainSession(row), nil
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	return s.q.DeleteSession(ctx, toPgUUID(id))
}

func toDomainUser(u User) domain.User {
	return domain.User{
		ID:        toUUID(u.ID),
		Subject:   u.Subject,
		UPN:       u.Upn,
		Email:     u.Email,
		Name:      u.Name,
		Role:      u.Role,
		LastLogin: u.LastLoginAt.Time,
		CreatedAt: u.CreatedAt.Time,
	}
}

func toDomainSession(s Session) domain.Session {
	return domain.Session{
		ID:        toUUID(s.ID),
		UserID:    toUUID(s.UserID),
		TokenHash: s.TokenHash,
		ExpiresAt: s.ExpiresAt.Time,
		CreatedAt: s.CreatedAt.Time,
	}
}
