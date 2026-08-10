package fakeports

import (
	"context"
	"strings"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) CreateSubmission(ctx context.Context, in domain.Submission) (domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.subs[in.ID] = in
	return in, nil
}

func (s *Store) GetSubmission(ctx context.Context, id uuid.UUID) (domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[id]
	if !ok {
		return domain.Submission{}, domain.ErrNotFound
	}
	return sub, nil
}

func (s *Store) FindSubmissionsBySourceChecksum(ctx context.Context, checksum string) ([]domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Submission
	for _, v := range s.subs {
		if v.SourceChecksum == checksum {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *Store) SetSubmissionStatus(ctx context.Context, id uuid.UUID, st domain.SubmissionStatus, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub, ok := s.subs[id]
	if !ok {
		return domain.ErrNotFound
	}
	sub.Status = st
	sub.FailedReason = reason
	s.subs[id] = sub
	return nil
}

func (s *Store) ListReceivedWithoutFixJob(ctx context.Context) ([]domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Submission
	for _, sub := range s.subs {
		if sub.Status != domain.SubmissionReceived {
			continue
		}
		if _, ok := s.jobs[jobKey(domain.JobFixSubmission, sub.ID)]; ok {
			continue
		}
		out = append(out, sub)
	}
	return out, nil
}

func (s *Store) ListSubmissions(ctx context.Context, status string, limit int) ([]domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Submission
	for _, sub := range s.subs {
		if status != "" && string(sub.Status) != status {
			continue
		}
		out = append(out, sub)
	}
	return out, nil
}

func (s *Store) ListSubmissionsFiltered(ctx context.Context, status, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Submission, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Submission
	for _, sub := range s.subs {
		if status != "" && string(sub.Status) != status {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(sub.Filename), strings.ToLower(search)) &&
			!strings.Contains(strings.ToLower(sub.SourceChecksum), strings.ToLower(search)) {
			continue
		}
		if start != nil && sub.ReceivedAt.Before(*start) {
			continue
		}
		if end != nil && sub.ReceivedAt.After(*end) {
			continue
		}
		out = append(out, sub)
	}
	sortSubmissionsFor(out, sort, dir)
	return applyPage(out, offset, limit), nil
}

func (s *Store) CountSubmissionsFiltered(ctx context.Context, status, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListSubmissionsFiltered(ctx, status, search, start, end, "", "", 1<<30, 0)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}
