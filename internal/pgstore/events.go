package pgstore

import (
	"context"
	"encoding/json"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func (s *Store) AppendEvent(ctx context.Context, typ string, ref *uuid.UUID, payload json.RawMessage) error {
	var refPg pgtype.UUID
	if ref != nil {
		refPg = toPgUUID(*ref)
	}
	_, err := s.q.AppendEvent(ctx, AppendEventParams{
		Type:    typ,
		Ref:     refPg,
		Payload: payload,
	})
	return err
}

func (s *Store) ListEvents(ctx context.Context, limit, offset int) ([]domain.Event, error) {
	rows, err := s.q.ListEvents(ctx, ListEventsParams{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainEvent(r))
	}
	return out, nil
}

func (s *Store) ListEventsFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Event, error) {
	rows, err := s.q.ListEventsFiltered(ctx, ListEventsFilteredParams{
		Column1: search, Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
		Column4: sort, Column5: dir,
		Limit: int32(limit), Offset: int32(offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Event, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainEvent(r))
	}
	return out, nil
}

func (s *Store) CountEventsFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error) {
	return s.q.CountEventsFiltered(ctx, CountEventsFilteredParams{
		Column1: search, Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
	})
}

func toDomainEvent(e Event) domain.Event {
	var ref *uuid.UUID
	if e.Ref.Valid {
		id := toUUID(e.Ref)
		ref = &id
	}
	return domain.Event{
		ID: toUUID(e.ID), Type: e.Type, Ref: ref,
		Payload: json.RawMessage(e.Payload), CreatedAt: e.CreatedAt.Time,
	}
}
