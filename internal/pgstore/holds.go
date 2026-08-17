package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) CreateHold(ctx context.Context, entryID uuid.UUID, status domain.HoldStatus, reason string) (domain.Hold, error) {
	row, err := s.q.CreateHold(ctx, CreateHoldParams{
		EntryID: toPgUUID(entryID),
		Status:  string(status),
		Reason:  reason,
	})
	if err != nil {
		return domain.Hold{}, err
	}
	return toDomainHold(row), nil
}

func (s *Store) ListHoldsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Hold, error) {
	rows, err := s.q.ListHoldsBySubmission(ctx, toPgUUID(submissionID))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Hold, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainHoldDetail(
			r.ID, r.EntryID, r.ReleaseArtifactID, r.Status, r.Reason,
			r.EntryTrace, r.EntryRdfi, r.EntryReceiverName, r.EntryReceiverAccount,
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.CompanyName, r.Filename, r.CreatedAt.Time))
	}
	return out, nil
}

func (s *Store) ListHoldsByReleaseArtifact(ctx context.Context, artifactID uuid.UUID) ([]domain.Hold, error) {
	rows, err := s.q.ListHoldsByReleaseArtifact(ctx, toPgUUID(artifactID))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Hold, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainHoldDetail(
			r.ID, r.EntryID, r.ReleaseArtifactID, r.Status, r.Reason,
			r.EntryTrace, r.EntryRdfi, r.EntryReceiverName, r.EntryReceiverAccount,
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.CompanyName, r.Filename, r.CreatedAt.Time))
	}
	return out, nil
}

func (s *Store) ListAllHolds(ctx context.Context) ([]domain.Hold, error) {
	rows, err := s.q.ListAllHolds(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Hold, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainHoldDetail(
			r.ID, r.EntryID, r.ReleaseArtifactID, r.Status, r.Reason,
			r.EntryTrace, r.EntryRdfi, r.EntryReceiverName, r.EntryReceiverAccount,
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.CompanyName, r.Filename, r.CreatedAt.Time))
	}
	return out, nil
}

func (s *Store) GetHold(ctx context.Context, id uuid.UUID) (domain.Hold, error) {
	row, err := s.q.GetHoldByID(ctx, toPgUUID(id))
	if err != nil {
		return domain.Hold{}, translate(err)
	}
	return toDomainHoldDetail(
		row.ID, row.EntryID, row.ReleaseArtifactID, row.Status, row.Reason,
		row.EntryTrace, row.EntryRdfi, row.EntryReceiverName, row.EntryReceiverAccount,
		row.EntryAmount, row.EntryTranCode, row.EffectiveDate, row.SubmissionID, row.CustomerID, row.CompanyName, row.Filename,
		row.CreatedAt.Time,
	), nil
}

func (s *Store) ListHoldsByStatus(ctx context.Context, status string, limit int) ([]domain.Hold, error) {
	rows, err := s.q.ListHoldsByStatus(ctx, ListHoldsByStatusParams{Column1: status, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Hold, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainHoldDetail(
			r.ID, r.EntryID, r.ReleaseArtifactID, r.Status, r.Reason,
			r.EntryTrace, r.EntryRdfi, r.EntryReceiverName, r.EntryReceiverAccount,
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.CompanyName, r.Filename,
			r.CreatedAt.Time,
		))
	}
	return out, nil
}

func (s *Store) CountHoldsByStatus(ctx context.Context, cutoff time.Time) (map[string]int64, error) {
	rows, err := s.q.CountHoldsByStatus(ctx, toPgTime(cutoff))
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Status] = r.Count
	}
	return out, nil
}

func (s *Store) ListHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Hold, error) {
	rows, err := s.q.ListHoldsFiltered(ctx, ListHoldsFilteredParams{
		Column1: status, Column2: search,
		Column3: toPgDatePtr(start), Column4: toPgDatePtr(end),
		Column5: sort, Column6: dir,
		Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Hold, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainHoldDetail(
			r.ID, r.EntryID, r.ReleaseArtifactID, r.Status, r.Reason,
			r.EntryTrace, r.EntryRdfi, r.EntryReceiverName, r.EntryReceiverAccount,
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.CompanyName, r.Filename,
			r.CreatedAt.Time,
		))
	}
	return out, nil
}

func (s *Store) CountHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time) (int64, error) {
	return s.q.CountHoldsFiltered(ctx, CountHoldsFilteredParams{
		Column1: status, Column2: search,
		Column3: toPgDatePtr(start), Column4: toPgDatePtr(end),
	})
}

func (s *Store) ListCombosBySubmission(ctx context.Context, submissionID uuid.UUID, cutoff time.Time) ([]domain.HoldCombo, error) {
	rows, err := s.q.ListCombosBySubmission(ctx, ListCombosBySubmissionParams{
		SubmissionID: toPgUUID(submissionID),
		Column2:      toPgDate(&cutoff),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.HoldCombo, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.HoldCombo{
			Rdfi:            r.Rdfi,
			ReceiverAccount: r.ReceiverAccount,
			HasApproved:     r.HasApproved,
			HasDeclined:     r.HasDeclined,
		})
	}
	return out, nil
}

func (s *Store) SetHoldStatus(ctx context.Context, id uuid.UUID, status domain.HoldStatus) error {
	return s.q.SetHoldStatus(ctx, SetHoldStatusParams{
		Status: string(status),
		ID:     toPgUUID(id),
	})
}

func (s *Store) SetHoldStatusIfOpen(ctx context.Context, id uuid.UUID, status domain.HoldStatus) (bool, error) {
	_, err := s.q.SetHoldStatusIfOpen(ctx, SetHoldStatusIfOpenParams{
		Status: string(status),
		ID:     toPgUUID(id),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *Store) SetHoldReleaseArtifact(ctx context.Context, holdID, artifactID uuid.UUID) error {
	return s.q.SetHoldReleaseArtifact(ctx, SetHoldReleaseArtifactParams{
		ReleaseArtifactID: toPgUUID(artifactID),
		ID:                toPgUUID(holdID),
	})
}

func (s *Store) CreateReview(ctx context.Context, in domain.Review) error {
	_, err := s.q.CreateReview(ctx, CreateReviewParams{
		HoldID: toPgUUID(in.HoldID),
		Actor:  in.Actor,
		Action: in.Action,
		Note:   in.Note,
	})
	return err
}

func (s *Store) ListReviewsByHold(ctx context.Context, holdID uuid.UUID) ([]domain.Review, error) {
	rows, err := s.q.ListReviewsByHold(ctx, toPgUUID(holdID))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Review, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.Review{
			ID: toUUID(r.ID), HoldID: toUUID(r.HoldID), Actor: r.Actor,
			Action: r.Action, Note: r.Note, CreatedAt: r.CreatedAt.Time,
		})
	}
	return out, nil
}

func toDomainHold(h Hold) domain.Hold {
	var rel *uuid.UUID
	if h.ReleaseArtifactID.Valid {
		id := toUUID(h.ReleaseArtifactID)
		rel = &id
	}
	return domain.Hold{
		ID:                toUUID(h.ID),
		EntryID:           toUUID(h.EntryID),
		Status:            domain.HoldStatus(h.Status),
		Reason:            h.Reason,
		ReleaseArtifactID: rel,
	}
}

func toDomainHoldDetail(
	id, entryID, release pgtype.UUID, status, reason, trace, rdfi, name, acct string,
	amount int64, tran int32, eff pgtype.Date, submissionID pgtype.UUID, customer, company, filename string,
	created time.Time,
) domain.Hold {
	h := toDomainHold(Hold{
		ID:                id,
		EntryID:           entryID,
		Status:            status,
		Reason:            reason,
		ReleaseArtifactID: release,
	})
	h.SubmissionID = toUUID(submissionID)
	h.CustomerID = customer
	h.CompanyName = company
	h.Filename = filename
	h.EntryTrace = trace
	h.EntryRdfi = rdfi
	h.EntryReceiverName = name
	h.EntryReceiverAcct = acct
	h.EntryAmount = amount
	h.EntryTranCode = int(tran)
	h.EffectiveDate = toDate(eff)
	h.CreatedAt = created
	return h
}
