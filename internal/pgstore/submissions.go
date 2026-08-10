package pgstore

import (
	"context"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) CreateSubmission(ctx context.Context, in domain.Submission) (domain.Submission, error) {
	row, err := s.q.CreateSubmission(ctx, CreateSubmissionParams{
		Filename:       in.Filename,
		SourceChecksum: in.SourceChecksum,
		Status:         string(in.Status),
		ReceivedAt:     toPgTime(in.ReceivedAt),
	})
	if err != nil {
		return domain.Submission{}, err
	}
	return toDomainSubmission(row), nil
}

func (s *Store) GetSubmission(ctx context.Context, id uuid.UUID) (domain.Submission, error) {
	row, err := s.q.GetSubmissionByID(ctx, toPgUUID(id))
	if err != nil {
		return domain.Submission{}, translate(err)
	}
	return toDomainSubmission(row), nil
}

func (s *Store) FindSubmissionsBySourceChecksum(ctx context.Context, checksum string) ([]domain.Submission, error) {
	rows, err := s.q.FindSubmissionsBySourceChecksum(ctx, checksum)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Submission, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainSubmission(r))
	}
	return out, nil
}

func (s *Store) SetSubmissionStatus(ctx context.Context, id uuid.UUID, st domain.SubmissionStatus, reason string) error {
	return s.q.SetSubmissionStatus(ctx, SetSubmissionStatusParams{
		Status:       string(st),
		FailedReason: reason,
		ID:           toPgUUID(id),
	})
}

func (s *Store) ListReceivedWithoutFixJob(ctx context.Context) ([]domain.Submission, error) {
	rows, err := s.q.ListReceivedWithoutFixJob(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Submission, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainSubmission(r))
	}
	return out, nil
}

func (s *Store) ListSubmissions(ctx context.Context, status string, limit int) ([]domain.Submission, error) {
	rows, err := s.q.ListSubmissions(ctx, ListSubmissionsParams{Column1: status, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Submission, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainSubmission(r))
	}
	return out, nil
}

func (s *Store) ListSubmissionsFiltered(ctx context.Context, status, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Submission, error) {
	rows, err := s.q.ListSubmissionsFiltered(ctx, ListSubmissionsFilteredParams{
		Column1: status, Column2: search,
		Column3: toPgDatePtr(start), Column4: toPgDatePtr(end),
		Column5: sort, Column6: dir,
		Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Submission, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainSubmission(r))
	}
	return out, nil
}

func (s *Store) CountSubmissionsFiltered(ctx context.Context, status, search string, start, end *time.Time) (int64, error) {
	return s.q.CountSubmissionsFiltered(ctx, CountSubmissionsFilteredParams{
		Column1: status, Column2: search,
		Column3: toPgDatePtr(start), Column4: toPgDatePtr(end),
	})
}

func toDomainSubmission(s Submission) domain.Submission {
	return domain.Submission{
		ID:             toUUID(s.ID),
		Filename:       s.Filename,
		SourceChecksum: s.SourceChecksum,
		Status:         domain.SubmissionStatus(s.Status),
		FailedReason:   s.FailedReason,
		ReceivedAt:     s.ReceivedAt.Time,
	}
}
