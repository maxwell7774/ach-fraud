package pgstore

import (
	"context"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

func (s *Store) ListRecipients(ctx context.Context) ([]domain.Recipient, error) {
	rows, err := s.q.ListRecipients(ctx)
	if err != nil {
		return nil, err
	}
	alerts, err := s.q.ListRecipientAlerts(ctx)
	if err != nil {
		return nil, err
	}
	byRecipient := map[uuid.UUID][]string{}
	for _, a := range alerts {
		rid := toUUID(a.RecipientID)
		byRecipient[rid] = append(byRecipient[rid], a.AlertType)
	}
	out := make([]domain.Recipient, 0, len(rows))
	for _, r := range rows {
		id := toUUID(r.ID)
		alertTypes := byRecipient[id]
		if alertTypes == nil {
			alertTypes = []string{}
		}
		out = append(out, domain.Recipient{
			ID:         id,
			Email:      r.Email,
			Name:       r.Name,
			Enabled:    r.Enabled,
			AlertTypes: alertTypes,
			CreatedAt:  r.CreatedAt.Time,
		})
	}
	return out, nil
}

func (s *Store) CreateRecipient(ctx context.Context, r domain.Recipient) (domain.Recipient, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Recipient{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	row, err := q.CreateRecipient(ctx, CreateRecipientParams{Email: r.Email, Name: r.Name, Enabled: r.Enabled})
	if err != nil {
		return domain.Recipient{}, err
	}
	r.ID = toUUID(row.ID)
	r.CreatedAt = row.CreatedAt.Time
	if err := setRecipientAlerts(ctx, q, r.ID, r.AlertTypes); err != nil {
		return domain.Recipient{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Recipient{}, err
	}
	return r, nil
}

func (s *Store) UpdateRecipient(ctx context.Context, r domain.Recipient) (domain.Recipient, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Recipient{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	row, err := q.UpdateRecipient(ctx, UpdateRecipientParams{
		Email:   r.Email,
		Name:    r.Name,
		Enabled: r.Enabled,
		ID:      toPgUUID(r.ID),
	})
	if err != nil {
		return domain.Recipient{}, err
	}
	r.Email = row.Email
	r.CreatedAt = row.CreatedAt.Time
	if r.AlertTypes != nil {
		if err := setRecipientAlerts(ctx, q, r.ID, r.AlertTypes); err != nil {
			return domain.Recipient{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Recipient{}, err
	}
	return r, nil
}

func (s *Store) DeleteRecipient(ctx context.Context, id uuid.UUID) error {
	return s.q.DeleteRecipient(ctx, toPgUUID(id))
}

// setRecipientAlerts replaces a recipient's alert-type rows with the given set.
func setRecipientAlerts(ctx context.Context, q *Queries, id uuid.UUID, types []string) error {
	if err := q.ClearRecipientAlerts(ctx, toPgUUID(id)); err != nil {
		return err
	}
	for _, t := range types {
		if err := q.AddRecipientAlert(ctx, AddRecipientAlertParams{
			RecipientID: toPgUUID(id),
			AlertType:   t,
		}); err != nil {
			return err
		}
	}
	return nil
}
