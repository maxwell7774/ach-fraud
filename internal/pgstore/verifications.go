package pgstore

import (
	"context"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) UpsertVerification(ctx context.Context, submissionID uuid.UUID, verified bool, issues string) error {
	_, err := s.q.UpsertVerification(ctx, UpsertVerificationParams{
		SubmissionID: toPgUUID(submissionID),
		Verified:     verified,
		Issues:       issues,
	})
	return err
}

func (s *Store) GetVerification(ctx context.Context, submissionID uuid.UUID) (domain.Verification, error) {
	row, err := s.q.GetVerificationBySubmission(ctx, toPgUUID(submissionID))
	if err != nil {
		return domain.Verification{}, translate(err)
	}
	return domain.Verification{
		SubmissionID: toUUID(row.SubmissionID),
		Verified:     row.Verified,
		Issues:       row.Issues,
		CheckedAt:    row.CheckedAt.Time,
	}, nil
}
