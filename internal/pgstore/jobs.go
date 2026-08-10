package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) EnqueueJob(ctx context.Context, kind domain.JobKind, ref uuid.UUID, runAt time.Time) error {
	return s.q.EnqueueJob(ctx, EnqueueJobParams{
		Kind:  string(kind),
		Ref:   toPgUUID(ref),
		RunAt: toPgTime(runAt),
	})
}

func (s *Store) ClaimDueJob(ctx context.Context) (*domain.Job, error) {
	row, err := s.q.ClaimDueJob(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j := toDomainJob(row)
	return &j, nil
}

func (s *Store) CompleteJob(ctx context.Context, id uuid.UUID) error {
	return s.q.CompleteJob(ctx, toPgUUID(id))
}

func (s *Store) FailJob(ctx context.Context, id uuid.UUID, reason string) error {
	return s.q.FailJob(ctx, FailJobParams{
		ID:        toPgUUID(id),
		LastError: reason,
	})
}

func (s *Store) RequeueJob(ctx context.Context, id uuid.UUID) error {
	_, err := s.q.RequeueJob(ctx, toPgUUID(id))
	return translate(err)
}

func (s *Store) RequeueStaleJobs(ctx context.Context) error {
	return s.q.RequeueStaleJobs(ctx)
}

func (s *Store) ListJobsByState(ctx context.Context, st domain.JobState, limit int) ([]domain.Job, error) {
	rows, err := s.q.ListJobsByState(ctx, ListJobsByStateParams{
		State: string(st),
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainJob(r))
	}
	return out, nil
}

func (s *Store) ListJobsByRef(ctx context.Context, ref uuid.UUID) ([]domain.Job, error) {
	rows, err := s.q.ListJobsByRef(ctx, toPgUUID(ref))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainJob(r))
	}
	return out, nil
}

func toDomainJob(j Job) domain.Job {
	return domain.Job{
		ID:        toUUID(j.ID),
		Kind:      domain.JobKind(j.Kind),
		Ref:       toUUID(j.Ref),
		State:     domain.JobState(j.State),
		Failures:  int(j.Failures),
		LastError: j.LastError,
		RunAt:     j.RunAt.Time,
	}
}
