package fakeports

import (
	"context"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) EnqueueJob(ctx context.Context, kind domain.JobKind, ref uuid.UUID, runAt time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := jobKey(kind, ref)
	if j, ok := s.jobs[key]; ok {
		j.RunAt = runAt
		j.State = domain.JobQueued
		j.LastError = ""
		s.updated[j.ID] = s.Now()
		return nil
	}
	j := &domain.Job{
		ID:    uuid.New(),
		Kind:  kind,
		Ref:   ref,
		State: domain.JobQueued,
		RunAt: runAt,
	}
	s.jobs[key] = j
	s.order = append(s.order, j.ID)
	s.updated[j.ID] = s.Now()
	return nil
}

func (s *Store) ClaimDueJob(ctx context.Context) (*domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for _, id := range s.order {
		var j *domain.Job
		for _, candidate := range s.jobs {
			if candidate.ID == id {
				j = candidate
				break
			}
		}
		if j == nil || j.State != domain.JobQueued || j.RunAt.After(now) {
			continue
		}
		j.State = domain.JobInProgress
		s.updated[j.ID] = s.Now()
		cp := *j
		return &cp, nil
	}
	return nil, nil
}

func (s *Store) CompleteJob(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.State = domain.JobDone
			s.updated[j.ID] = s.Now()
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) FailJob(ctx context.Context, id uuid.UUID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.State = domain.JobFailed
			j.LastError = reason
			j.Failures++
			s.updated[j.ID] = s.Now()
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) RequeueJob(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if j.ID == id {
			j.State = domain.JobQueued
			j.LastError = ""
			j.RunAt = s.Now()
			s.updated[j.ID] = s.Now()
			return nil
		}
	}
	return domain.ErrNotFound
}

func (s *Store) RequeueStaleJobs(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for _, j := range s.jobs {
		if j.State == domain.JobInProgress && now.Sub(s.updated[j.ID]) > 5*time.Minute {
			j.State = domain.JobQueued
		}
	}
	return nil
}

func (s *Store) ListJobsByState(ctx context.Context, st domain.JobState, limit int) ([]domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Job
	for _, j := range s.jobs {
		if j.State == st {
			out = append(out, *j)
		}
	}
	return out, nil
}

func (s *Store) ListJobsByRef(ctx context.Context, ref uuid.UUID) ([]domain.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Job
	for _, j := range s.jobs {
		if j.Ref == ref {
			out = append(out, *j)
		}
	}
	return out, nil
}
