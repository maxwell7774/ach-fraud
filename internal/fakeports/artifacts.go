package fakeports

import (
	"context"
	"fmt"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) CreateArtifact(ctx context.Context, in domain.Artifact) (domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.arts {
		if a.SubmissionID == in.SubmissionID && a.Kind == in.Kind && in.Kind != domain.ArtifactRelease {
			return domain.Artifact{}, fmt.Errorf("duplicate artifact (submission %s, kind %s)", in.SubmissionID, in.Kind)
		}
	}
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.arts[in.ID] = in
	s.artUpd[in.ID] = s.Now()
	return in, nil
}

func (s *Store) GetArtifactBySubmissionKind(ctx context.Context, submissionID uuid.UUID, kind domain.ArtifactKind) (domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, a := range s.arts {
		if a.SubmissionID == submissionID && a.Kind == kind {
			return a, nil
		}
	}
	return domain.Artifact{}, domain.ErrNotFound
}

func (s *Store) GetArtifactByID(ctx context.Context, id uuid.UUID) (domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.arts[id]
	if !ok {
		return domain.Artifact{}, domain.ErrNotFound
	}
	return a, nil
}

func (s *Store) ListArtifactsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Artifact, error) {
	return s.ArtifactsFor(submissionID), nil
}

func (s *Store) SetArtifactState(ctx context.Context, id uuid.UUID, st domain.ArtifactState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.arts[id]
	if !ok {
		return domain.ErrNotFound
	}
	a.State = st
	s.arts[id] = a
	s.artUpd[id] = s.Now()
	return nil
}

func (s *Store) ListArtifactsByStateOlderThan(ctx context.Context, state domain.ArtifactState, cutoff time.Time) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Artifact
	for _, a := range s.arts {
		if a.State != state {
			continue
		}
		updated, ok := s.artUpd[a.ID]
		if !ok || !updated.Before(cutoff) {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// BackdateArtifacts moves every artifact's recorded updated_at back by d, so
// prune tests can make seeded artifacts older than the retention cutoff.
func (s *Store) BackdateArtifacts(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, t := range s.artUpd {
		s.artUpd[id] = t.Add(-d)
	}
}

func (s *Store) ListArtifactsByChecksum(ctx context.Context, checksum string) ([]domain.Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Artifact
	for _, a := range s.arts {
		if a.Checksum == checksum {
			out = append(out, a)
		}
	}
	return out, nil
}
