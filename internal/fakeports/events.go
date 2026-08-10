package fakeports

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) ListEvents(ctx context.Context, limit, offset int) ([]domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Event
	for i := len(s.events) - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.events[i])
	}
	return out, nil
}

func (s *Store) ListEventsFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Event
	for _, e := range s.events {
		if search != "" && !strings.Contains(strings.ToLower(e.Type), strings.ToLower(search)) {
			continue
		}
		if start != nil && e.CreatedAt.Before(*start) {
			continue
		}
		if end != nil && e.CreatedAt.After(*end) {
			continue
		}
		out = append(out, e)
	}
	sortEventsFor(out, sort, dir)
	return applyPage(out, offset, limit), nil
}

func (s *Store) CountEventsFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	rows, err := s.ListEventsFiltered(ctx, search, start, end, "", "", 1<<30, 0)
	if err != nil {
		return 0, err
	}
	return int64(len(rows)), nil
}

func (s *Store) AppendEvent(ctx context.Context, typ string, ref *uuid.UUID, payload json.RawMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, domain.Event{ID: uuid.New(), Type: typ, Ref: ref, Payload: payload, CreatedAt: time.Now()})
	return nil
}
