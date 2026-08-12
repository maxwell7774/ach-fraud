package fakeports

import (
	"context"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) UpsertVerification(ctx context.Context, submissionID uuid.UUID, verified bool, issues string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.verifs[submissionID] = domain.Verification{
		SubmissionID: submissionID,
		Verified:     verified,
		Issues:       issues,
		CheckedAt:    s.Now(),
	}
	return nil
}

func (s *Store) GetVerification(ctx context.Context, submissionID uuid.UUID) (domain.Verification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.verifs[submissionID]
	if !ok {
		return domain.Verification{}, domain.ErrNotFound
	}
	return v, nil
}
