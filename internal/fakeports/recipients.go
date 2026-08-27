package fakeports

import (
	"context"
	"slices"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) ListRecipients(ctx context.Context) ([]domain.Recipient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Recipient, 0, len(s.recips))
	for _, r := range s.recips {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b domain.Recipient) int {
		switch {
		case a.Email < b.Email:
			return -1
		case a.Email > b.Email:
			return 1
		}
		return 0
	})
	return out, nil
}

func (s *Store) CreateRecipient(ctx context.Context, r domain.Recipient) (domain.Recipient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = uuid.New()
	r.CreatedAt = s.Now()
	s.recips[r.ID] = r
	return r, nil
}

func (s *Store) UpdateRecipient(ctx context.Context, r domain.Recipient) (domain.Recipient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, ok := s.recips[r.ID]
	if !ok {
		return domain.Recipient{}, domain.ErrNotFound
	}
	existing.Name = r.Name
	existing.Email = r.Email
	existing.Enabled = r.Enabled
	if r.AlertTypes != nil {
		existing.AlertTypes = r.AlertTypes
	}
	s.recips[r.ID] = existing
	return existing, nil
}

func (s *Store) DeleteRecipient(ctx context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.recips[id]; !ok {
		return domain.ErrNotFound
	}
	delete(s.recips, id)
	return nil
}
