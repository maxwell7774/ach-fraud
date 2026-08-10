package fakeports

import (
	"context"
	"strings"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) CreateBatchHeader(ctx context.Context, in domain.BatchHeader) (domain.BatchHeader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.headers[in.ID] = in
	return in, nil
}

func (s *Store) CreateBatchEntry(ctx context.Context, in domain.BatchEntry) (domain.BatchEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if in.ID == uuid.Nil {
		in.ID = uuid.New()
	}
	s.entries[in.ID] = in
	return in, nil
}

func (s *Store) ListEntriesBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.BatchEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.BatchEntry
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		if hdr.SubmissionID != submissionID {
			continue
		}
		e.EffectiveDate = hdr.EffectiveDate
		e.CustomerID = hdr.CustomerID
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) HasEntryByRdfiAccount(ctx context.Context, rdfi, account string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.Rdfi == rdfi && e.ReceiverAccount == account {
			return true, nil
		}
	}
	return false, nil
}

func (s *Store) SumVelocity(ctx context.Context, cutoff time.Time, submissionID uuid.UUID) ([]domain.VelocitySum, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Restrict to the velocity groups present in the submission being screened.
	targets := map[string]bool{}
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		if hdr.SubmissionID != submissionID {
			continue
		}
		if e.TranCode != 22 && e.TranCode != 32 {
			continue
		}
		if hdr.EffectiveDate == nil || hdr.EffectiveDate.Before(cutoff) {
			continue
		}
		targets[velocityKeyFor(e.ReceiverAccount, e.Rdfi, hdr.CustomerID, hdr.EffectiveDate)] = true
	}

	sums := map[string]*domain.VelocitySum{}
	order := []string{}
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		sub := s.subs[hdr.SubmissionID]
		// Ready and archived submissions both carry real same-day exposure.
		if sub.Status != domain.SubmissionReady && sub.Status != domain.SubmissionArchived {
			continue
		}
		if j, ok := s.jobs[jobKey(domain.JobProcess, sub.ID)]; ok && j.State == domain.JobFailed {
			continue
		}
		if e.TranCode != 22 && e.TranCode != 32 {
			continue
		}
		if hdr.EffectiveDate == nil || hdr.EffectiveDate.Before(cutoff) {
			continue
		}
		key := velocityKeyFor(e.ReceiverAccount, e.Rdfi, hdr.CustomerID, hdr.EffectiveDate)
		if !targets[key] {
			continue
		}
		vs, ok := sums[key]
		if !ok {
			vs = &domain.VelocitySum{
				ReceiverAccount: e.ReceiverAccount,
				Rdfi:            e.Rdfi,
				EffectiveDate:   hdr.EffectiveDate,
				CustomerID:      hdr.CustomerID,
			}
			sums[key] = vs
			order = append(order, key)
		}
		vs.Total += e.Amount
	}
	out := make([]domain.VelocitySum, 0, len(order))
	for _, k := range order {
		out = append(out, *sums[k])
	}
	return out, nil
}

func velocityKeyFor(account, rdfi, customer string, eff *time.Time) string {
	day := ""
	if eff != nil {
		day = eff.Format("2006-01-02")
	}
	return account + "|" + rdfi + "|" + day + "|" + customer
}

func (s *Store) ListEntriesFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.BatchEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	search = strings.ToLower(search)
	var out []domain.BatchEntry
	for _, e := range s.entries {
		hdr := s.headers[e.HeaderID]
		sub := s.subs[hdr.SubmissionID]
		filename := sub.Filename
		e.EffectiveDate = hdr.EffectiveDate
		e.CustomerID = hdr.CustomerID
		e.Filename = filename
		if search != "" {
			fields := []string{e.Trace, e.Rdfi, e.ReceiverName, e.ReceiverAccount, filename}
			found := false
			for _, v := range fields {
				if strings.Contains(strings.ToLower(v), search) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if start != nil && (hdr.EffectiveDate == nil || hdr.EffectiveDate.Before(*start)) {
			continue
		}
		if end != nil && (hdr.EffectiveDate != nil && hdr.EffectiveDate.After(*end)) {
			continue
		}
		out = append(out, e)
	}
	sortEntriesFor(out, sort, dir)
	return applyPage(out, offset, limit), nil
}

func (s *Store) CountEntriesFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListEntriesFiltered(ctx, search, start, end, "", "", 100000, 0)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

func (s *Store) ListHeadersFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.BatchHeader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	search = strings.ToLower(search)
	var out []domain.BatchHeader
	for _, h := range s.headers {
		sub := s.subs[h.SubmissionID]
		h.Filename = sub.Filename
		if search != "" {
			fields := []string{h.CompanyName, h.CustomerID, h.CompanyDescription, sub.Filename}
			found := false
			for _, v := range fields {
				if strings.Contains(strings.ToLower(v), search) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		if start != nil && (h.EffectiveDate == nil || h.EffectiveDate.Before(*start)) {
			continue
		}
		if end != nil && (h.EffectiveDate != nil && h.EffectiveDate.After(*end)) {
			continue
		}
		out = append(out, h)
	}
	sortHeadersFor(out, sort, dir)
	return applyPage(out, offset, limit), nil
}

func (s *Store) CountHeadersFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListHeadersFiltered(ctx, search, start, end, "", "", 100000, 0)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}
