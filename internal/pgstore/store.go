// Package pgstore implements ports.Store over Postgres using the
// sqlc-generated queries. It is the only package that may import the database
// driver; it translates between pgtype values and the domain model.
package pgstore

import (
	"context"
	"errors"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store adapts ports.Store to Postgres.
type Store struct {
	pool *pgxpool.Pool
	q    *Queries
}

// Open connects to Postgres and returns a Store. Call Close when done.
func Open(ctx context.Context, dbURL string) (*Store, error) {
	poolCfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool, q: New(pool)}, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) WithinTx(ctx context.Context, fn func(tx ports.Store) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(&Store{pool: s.pool, q: s.q.WithTx(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ingestLockKey is the advisory-lock key that serializes ingest registration
// across concurrent processes.
const ingestLockKey int64 = 724669123

func (s *Store) WithIngestLock(ctx context.Context, fn func(tx ports.Store) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := s.q.WithTx(tx).AcquireIngestLock(ctx, ingestLockKey); err != nil {
		return err
	}
	if err := fn(&Store{pool: s.pool, q: s.q.WithTx(tx)}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// translate maps driver errors onto domain errors.
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func toUUID(p pgtype.UUID) uuid.UUID {
	return uuid.UUID(p.Bytes)
}

func toPgUUID(u uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: u, Valid: true}
}

func toPgTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

func toDate(d pgtype.Date) *time.Time {
	if !d.Valid {
		return nil
	}
	t := d.Time
	return &t
}

func toPgDate(t *time.Time) pgtype.Date {
	if t == nil {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: *t, Valid: true}
}

func toPgDatePtr(t *time.Time) pgtype.Date {
	return toPgDate(t)
}

var _ ports.Store = (*Store)(nil)
