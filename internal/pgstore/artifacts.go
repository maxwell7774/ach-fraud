package pgstore

import (
	"context"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) CreateArtifact(ctx context.Context, in domain.Artifact) (domain.Artifact, error) {
	row, err := s.q.CreateArtifact(ctx, CreateArtifactParams{
		SubmissionID:  toPgUUID(in.SubmissionID),
		Kind:          string(in.Kind),
		Checksum:      in.Checksum,
		State:         string(in.State),
		DebitTotal:    toPgInt8(in.DebitTotal),
		CreditTotal:   toPgInt8(in.CreditTotal),
		DebitEntries:  toPgInt4(in.DebitEntries),
		CreditEntries: toPgInt4(in.CreditEntries),
	})
	if err != nil {
		return domain.Artifact{}, err
	}
	return toDomainArtifact(row), nil
}

func (s *Store) GetArtifactBySubmissionKind(ctx context.Context, submissionID uuid.UUID, kind domain.ArtifactKind) (domain.Artifact, error) {
	row, err := s.q.GetArtifactBySubmissionKind(ctx, GetArtifactBySubmissionKindParams{
		SubmissionID: toPgUUID(submissionID),
		Kind:         string(kind),
	})
	if err != nil {
		return domain.Artifact{}, translate(err)
	}
	return toDomainArtifact(row), nil
}

func (s *Store) GetArtifactByID(ctx context.Context, id uuid.UUID) (domain.Artifact, error) {
	row, err := s.q.GetArtifactByID(ctx, toPgUUID(id))
	if err != nil {
		return domain.Artifact{}, translate(err)
	}
	return toDomainArtifact(row), nil
}

func (s *Store) ListArtifactsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Artifact, error) {
	rows, err := s.q.ListArtifactsBySubmission(ctx, toPgUUID(submissionID))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Artifact, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainArtifact(r))
	}
	return out, nil
}

func (s *Store) SetArtifactState(ctx context.Context, id uuid.UUID, st domain.ArtifactState) error {
	return s.q.SetArtifactState(ctx, SetArtifactStateParams{
		State: string(st),
		ID:    toPgUUID(id),
	})
}

func (s *Store) SetArtifactTotals(ctx context.Context, id uuid.UUID, debitTotal, creditTotal int64, debitEntries, creditEntries int) error {
	return s.q.SetArtifactTotals(ctx, SetArtifactTotalsParams{
		DebitTotal:    toPgInt8(&debitTotal),
		CreditTotal:   toPgInt8(&creditTotal),
		DebitEntries:  toPgInt4(&debitEntries),
		CreditEntries: toPgInt4(&creditEntries),
		ID:            toPgUUID(id),
	})
}

func (s *Store) ListArtifactsByStateOlderThan(ctx context.Context, state domain.ArtifactState, cutoff time.Time) ([]domain.Artifact, error) {
	rows, err := s.q.ListArtifactsByStateOlderThan(ctx, ListArtifactsByStateOlderThanParams{
		State:     string(state),
		UpdatedAt: toPgTime(cutoff),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Artifact, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainArtifact(r))
	}
	return out, nil
}

func (s *Store) ListArtifactsByChecksum(ctx context.Context, checksum string) ([]domain.Artifact, error) {
	rows, err := s.q.ListArtifactsByChecksum(ctx, checksum)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Artifact, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainArtifact(r))
	}
	return out, nil
}

func toDomainArtifact(a Artifact) domain.Artifact {
	return domain.Artifact{
		ID:            toUUID(a.ID),
		SubmissionID:  toUUID(a.SubmissionID),
		Kind:          domain.ArtifactKind(a.Kind),
		Checksum:      a.Checksum,
		State:         domain.ArtifactState(a.State),
		DebitTotal:    fromPgInt8(a.DebitTotal),
		CreditTotal:   fromPgInt8(a.CreditTotal),
		DebitEntries:  fromPgInt4(a.DebitEntries),
		CreditEntries: fromPgInt4(a.CreditEntries),
	}
}
