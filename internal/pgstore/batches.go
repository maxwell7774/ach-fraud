package pgstore

import (
	"context"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) CreateBatchHeader(ctx context.Context, in domain.BatchHeader) (domain.BatchHeader, error) {
	row, err := s.q.CreateBatchHeader(ctx, CreateBatchHeaderParams{
		SubmissionID:       toPgUUID(in.SubmissionID),
		CustomerID:         in.CustomerID,
		CompanyName:        in.CompanyName,
		CompanyDescription: in.CompanyDescription,
		EffectiveDate:      toPgDate(in.EffectiveDate),
	})
	if err != nil {
		return domain.BatchHeader{}, err
	}
	return toDomainBatchHeader(row), nil
}

func (s *Store) CreateBatchEntry(ctx context.Context, in domain.BatchEntry) (domain.BatchEntry, error) {
	row, err := s.q.CreateBatchEntry(ctx, CreateBatchEntryParams{
		HeaderID:        toPgUUID(in.HeaderID),
		Rdfi:            in.Rdfi,
		ReceiverName:    in.ReceiverName,
		ReceiverAccount: in.ReceiverAccount,
		Amount:          in.Amount,
		TranCode:        int32(in.TranCode),
		Trace:           in.Trace,
	})
	if err != nil {
		return domain.BatchEntry{}, err
	}
	return toDomainBatchEntry(row), nil
}

func (s *Store) HasEntryByRdfiAccount(ctx context.Context, rdfi, account string) (bool, error) {
	return s.q.HasEntryByRdfiAccount(ctx, HasEntryByRdfiAccountParams{Rdfi: rdfi, ReceiverAccount: account})
}

func (s *Store) ListEntriesBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.BatchEntry, error) {
	rows, err := s.q.ListEntriesBySubmission(ctx, toPgUUID(submissionID))
	if err != nil {
		return nil, err
	}
	out := make([]domain.BatchEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.BatchEntry{
			ID:              toUUID(r.ID),
			HeaderID:        toUUID(r.HeaderID),
			Rdfi:            r.Rdfi,
			ReceiverName:    r.ReceiverName,
			ReceiverAccount: r.ReceiverAccount,
			Amount:          r.Amount,
			TranCode:        int(r.TranCode),
			Trace:           r.Trace,
			EffectiveDate:   toDate(r.EffectiveDate),
			CustomerID:      r.CustomerID,
		})
	}
	return out, nil
}

func (s *Store) SumVelocity(ctx context.Context, cutoff time.Time, submissionID uuid.UUID) ([]domain.VelocitySum, error) {
	rows, err := s.q.SumVelocity(ctx, SumVelocityParams{
		Column1:      toPgDate(&cutoff),
		SubmissionID: toPgUUID(submissionID),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.VelocitySum, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.VelocitySum{
			ReceiverAccount: r.ReceiverAccount,
			Rdfi:            r.Rdfi,
			EffectiveDate:   toDate(r.EffectiveDate),
			CustomerID:      r.CustomerID,
			Total:           r.Total,
		})
	}
	return out, nil
}

func (s *Store) SumHeldByGroup(ctx context.Context, cutoff time.Time, submissionID uuid.UUID) ([]domain.VelocitySum, error) {
	rows, err := s.q.SumHeldByGroup(ctx, SumHeldByGroupParams{
		Column1: toPgDate(&cutoff),
		ID:      toPgUUID(submissionID),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.VelocitySum, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.VelocitySum{
			ReceiverAccount: r.ReceiverAccount,
			Rdfi:            r.Rdfi,
			EffectiveDate:   toDate(r.EffectiveDate),
			CustomerID:      r.CustomerID,
			Total:           r.Total,
		})
	}
	return out, nil
}

func (s *Store) ListEntriesFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.BatchEntry, error) {
	rows, err := s.q.ListEntriesFiltered(ctx, ListEntriesFilteredParams{
		Column1: search,
		Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
		Column4: sort, Column5: dir,
		Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.BatchEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.BatchEntry{
			ID: toUUID(r.ID), HeaderID: toUUID(r.HeaderID),
			Rdfi: r.Rdfi, ReceiverName: r.ReceiverName, ReceiverAccount: r.ReceiverAccount,
			Amount: r.Amount, TranCode: int(r.TranCode), Trace: r.Trace,
			EffectiveDate: toDate(r.EffectiveDate), CustomerID: r.CustomerID, Filename: r.Filename,
		})
	}
	return out, nil
}

func (s *Store) CountEntriesFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	return s.q.CountEntriesFiltered(ctx, CountEntriesFilteredParams{
		Column1: search,
		Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
	})
}

func (s *Store) ListHeadersFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.BatchHeader, error) {
	rows, err := s.q.ListHeadersFiltered(ctx, ListHeadersFilteredParams{
		Column1: search,
		Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
		Column4: sort, Column5: dir,
		Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.BatchHeader, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.BatchHeader{
			ID: toUUID(r.ID), SubmissionID: toUUID(r.SubmissionID),
			CustomerID: r.CustomerID, CompanyName: r.CompanyName,
			CompanyDescription: r.CompanyDescription, EffectiveDate: toDate(r.EffectiveDate),
			Filename: r.Filename,
		})
	}
	return out, nil
}

func (s *Store) CountHeadersFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	return s.q.CountHeadersFiltered(ctx, CountHeadersFilteredParams{
		Column1: search,
		Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
	})
}

func toDomainBatchHeader(h BatchHeader) domain.BatchHeader {
	return domain.BatchHeader{
		ID:                 toUUID(h.ID),
		SubmissionID:       toUUID(h.SubmissionID),
		CustomerID:         h.CustomerID,
		CompanyName:        h.CompanyName,
		CompanyDescription: h.CompanyDescription,
		EffectiveDate:      toDate(h.EffectiveDate),
	}
}

func toDomainBatchEntry(e BatchEntry) domain.BatchEntry {
	return domain.BatchEntry{
		ID:              toUUID(e.ID),
		HeaderID:        toUUID(e.HeaderID),
		Rdfi:            e.Rdfi,
		ReceiverName:    e.ReceiverName,
		ReceiverAccount: e.ReceiverAccount,
		Amount:          e.Amount,
		TranCode:        int(e.TranCode),
		Trace:           e.Trace,
	}
}
