package fakeports

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) populateHold(h domain.Hold) domain.Hold {
	en, ok := s.entries[h.EntryID]
	if ok {
		h.EntryTrace = en.Trace
		h.EntryRdfi = en.Rdfi
		h.EntryReceiverName = en.ReceiverName
		h.EntryReceiverAcct = en.ReceiverAccount
		h.EntryAmount = en.Amount
		h.EntryTranCode = en.TranCode
		hdr := s.headers[en.HeaderID]
		h.EffectiveDate = hdr.EffectiveDate
		h.SubmissionID = hdr.SubmissionID
		h.CustomerID = hdr.CustomerID
		h.CompanyName = hdr.CompanyName
		if sub, ok := s.subs[hdr.SubmissionID]; ok {
			h.Filename = sub.Filename
		}
	}
	return h
}

func (s *Store) CreateHold(ctx context.Context, entryID uuid.UUID, status domain.HoldStatus, reason string) (domain.Hold, error) {
	if s.HoldError != nil {
		if err := s.HoldError(entryID); err != nil {
			return domain.Hold{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, h := range s.holds {
		if h.EntryID == entryID {
			return domain.Hold{}, fmt.Errorf("hold for entry %s already exists", entryID)
		}
	}
	h := domain.Hold{ID: uuid.New(), EntryID: entryID, Status: status, Reason: reason}
	s.holds[h.ID] = h
	s.holdUpd[h.ID] = s.Now()
	return h, nil
}

func (s *Store) ListHoldsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		p := s.populateHold(h)
		if p.SubmissionID == submissionID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (s *Store) ListHoldsByReleaseArtifact(ctx context.Context, artifactID uuid.UUID) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		if h.ReleaseArtifactID != nil && *h.ReleaseArtifactID == artifactID {
			out = append(out, s.populateHold(h))
		}
	}
	return out, nil
}

func (s *Store) ListAllHolds(ctx context.Context) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		out = append(out, s.populateHold(h))
	}
	return out, nil
}

func (s *Store) GetHold(ctx context.Context, id uuid.UUID) (domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return domain.Hold{}, domain.ErrNotFound
	}
	return s.populateHold(h), nil
}

func (s *Store) ListHoldsByStatus(ctx context.Context, status string, limit int) ([]domain.Hold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Hold
	for _, h := range s.holds {
		if status != "" && string(h.Status) != status {
			continue
		}
		out = append(out, s.populateHold(h))
	}
	return out, nil
}

func (s *Store) ListHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Hold, error) {
	rows, err := s.ListHoldsByStatus(ctx, status, 100000)
	if err != nil {
		return nil, err
	}
	search = strings.ToLower(search)
	var out []domain.Hold
	for _, h := range rows {
		if !matchesHold(h, search) {
			continue
		}
		if start != nil && (h.EffectiveDate == nil || h.EffectiveDate.Before(*start)) {
			continue
		}
		if end != nil && (h.EffectiveDate != nil && h.EffectiveDate.After(*end)) {
			continue
		}
		out = append(out, h)
	}
	sortHoldsFor(out, sort, dir)
	return applyPage(out, offset, limit), nil
}

func matchesHold(h domain.Hold, search string) bool {
	if search == "" {
		return true
	}
	for _, v := range []string{h.Filename, h.EntryReceiverName, h.EntryReceiverAcct, h.EntryTrace} {
		if strings.Contains(strings.ToLower(v), search) {
			return true
		}
	}
	return false
}

func (s *Store) CountHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListHoldsFiltered(ctx, status, search, start, end, "", "", 100000, 0)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

func (s *Store) CountHoldsByStatus(ctx context.Context, cutoff time.Time) (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]int64{}
	for _, h := range s.holds {
		// Open pending holds always count (the live review queue); resolved
		// statuses count only when decided since the cutoff.
		if h.Status != domain.HoldPending {
			upd, ok := s.holdUpd[h.ID]
			if !ok || upd.Before(cutoff) {
				continue
			}
		}
		out[string(h.Status)]++
	}
	return out, nil
}

func (s *Store) ListCombosBySubmission(ctx context.Context, submissionID uuid.UUID, cutoff time.Time) ([]domain.HoldCombo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Distinct eligible combos present in the submission being screened.
	combos := map[string]*domain.HoldCombo{}
	order := []string{}
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
		key := e.Rdfi + "|" + e.ReceiverAccount
		if _, ok := combos[key]; !ok {
			combos[key] = &domain.HoldCombo{Rdfi: e.Rdfi, ReceiverAccount: e.ReceiverAccount}
			order = append(order, key)
		}
	}

	// Full hold history (no expiry) consulted only for those pairs. A combo is
	// whitelisted by a prior approved hold, blacklisted by a prior declined
	// hold; pending/auto_declined count for neither.
	hasApproved := map[string]bool{}
	hasDeclined := map[string]bool{}
	for _, h := range s.holds {
		en := s.entries[h.EntryID]
		key := en.Rdfi + "|" + en.ReceiverAccount
		switch h.Status {
		case domain.HoldApproved:
			hasApproved[key] = true
		case domain.HoldDeclined:
			hasDeclined[key] = true
		}
	}

	out := make([]domain.HoldCombo, 0, len(order))
	for _, k := range order {
		c := combos[k]
		c.HasApproved = hasApproved[k]
		c.HasDeclined = hasDeclined[k]
		out = append(out, *c)
	}
	return out, nil
}

func (s *Store) SetHoldStatus(ctx context.Context, id uuid.UUID, status domain.HoldStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[id]
	if !ok {
		return domain.ErrNotFound
	}
	h.Status = status
	s.holds[id] = h
	s.holdUpd[id] = s.Now()
	return nil
}

func (s *Store) SetHoldReleaseArtifact(ctx context.Context, holdID, artifactID uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.holds[holdID]
	if !ok {
		return domain.ErrNotFound
	}
	id := artifactID
	h.ReleaseArtifactID = &id
	s.holds[holdID] = h
	return nil
}

func (s *Store) CreateReview(ctx context.Context, r domain.Review) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = uuid.New()
	r.CreatedAt = time.Now()
	s.reviews = append(s.reviews, r)
	return nil
}

func (s *Store) ListReviewsByHold(ctx context.Context, holdID uuid.UUID) ([]domain.Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Review
	for _, r := range s.reviews {
		if r.HoldID == holdID {
			out = append(out, r)
		}
	}
	return out, nil
}
