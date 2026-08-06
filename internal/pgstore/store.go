// Package pgstore implements ports.Store over Postgres using the
// sqlc-generated queries. It is the only package that may import the database
// driver; it translates between pgtype values and the domain model.
package pgstore

import (
	"context"
	"encoding/json"
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

func (s *Store) CreateSubmission(ctx context.Context, in domain.Submission) (domain.Submission, error) {
	row, err := s.q.CreateSubmission(ctx, CreateSubmissionParams{
		Filename:       in.Filename,
		SourceChecksum: in.SourceChecksum,
		Status:         string(in.Status),
		ReceivedAt:     toPgTime(in.ReceivedAt),
	})
	if err != nil {
		return domain.Submission{}, err
	}
	return toDomainSubmission(row), nil
}

func (s *Store) GetSubmission(ctx context.Context, id uuid.UUID) (domain.Submission, error) {
	row, err := s.q.GetSubmissionByID(ctx, toPgUUID(id))
	if err != nil {
		return domain.Submission{}, translate(err)
	}
	return toDomainSubmission(row), nil
}

func (s *Store) FindSubmissionsBySourceChecksum(ctx context.Context, checksum string) ([]domain.Submission, error) {
	rows, err := s.q.FindSubmissionsBySourceChecksum(ctx, checksum)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Submission, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainSubmission(r))
	}
	return out, nil
}

func (s *Store) SetSubmissionStatus(ctx context.Context, id uuid.UUID, st domain.SubmissionStatus, reason string) error {
	return s.q.SetSubmissionStatus(ctx, SetSubmissionStatusParams{
		Status:       string(st),
		FailedReason: reason,
		ID:           toPgUUID(id),
	})
}

func (s *Store) ListReceivedWithoutFixJob(ctx context.Context) ([]domain.Submission, error) {
	rows, err := s.q.ListReceivedWithoutFixJob(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Submission, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainSubmission(r))
	}
	return out, nil
}

func (s *Store) CreateArtifact(ctx context.Context, in domain.Artifact) (domain.Artifact, error) {
	row, err := s.q.CreateArtifact(ctx, CreateArtifactParams{
		SubmissionID: toPgUUID(in.SubmissionID),
		Kind:         string(in.Kind),
		Checksum:     in.Checksum,
		State:        string(in.State),
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
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.Filename, r.CreatedAt.Time))
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
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.Filename, r.CreatedAt.Time))
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
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.Filename, r.CreatedAt.Time))
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
		row.EntryAmount, row.EntryTranCode, row.EffectiveDate, row.SubmissionID, row.CustomerID, row.Filename,
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
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.Filename,
			r.CreatedAt.Time,
		))
	}
	return out, nil
}

func (s *Store) CountHoldsByStatus(ctx context.Context) (map[string]int64, error) {
	rows, err := s.q.CountHoldsByStatus(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Status] = r.Count
	}
	return out, nil
}

func (s *Store) ListHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time, limit int) ([]domain.Hold, error) {
	rows, err := s.q.ListHoldsFiltered(ctx, ListHoldsFilteredParams{
		Column1: status, Column2: search,
		Column3: toPgDatePtr(start), Column4: toPgDatePtr(end),
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Hold, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainHoldDetail(
			r.ID, r.EntryID, r.ReleaseArtifactID, r.Status, r.Reason,
			r.EntryTrace, r.EntryRdfi, r.EntryReceiverName, r.EntryReceiverAccount,
			r.EntryAmount, r.EntryTranCode, r.EffectiveDate, r.SubmissionID, r.CustomerID, r.Filename,
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

func (s *Store) ListEntriesFiltered(ctx context.Context, search string, start, end *time.Time, limit int) ([]domain.BatchEntry, error) {
	rows, err := s.q.ListEntriesFiltered(ctx, ListEntriesFilteredParams{
		Column1: search,
		Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
		Limit: int32(limit),
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

func (s *Store) ListHeadersFiltered(ctx context.Context, search string, start, end *time.Time, limit int) ([]domain.BatchHeader, error) {
	rows, err := s.q.ListHeadersFiltered(ctx, ListHeadersFilteredParams{
		Column1: search,
		Column2: toPgDatePtr(start), Column3: toPgDatePtr(end),
		Limit: int32(limit),
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

func (s *Store) GetArtifactByID(ctx context.Context, id uuid.UUID) (domain.Artifact, error) {
	row, err := s.q.GetArtifactByID(ctx, toPgUUID(id))
	if err != nil {
		return domain.Artifact{}, translate(err)
	}
	return toDomainArtifact(row), nil
}

func (s *Store) ListSubmissions(ctx context.Context, status string, limit int) ([]domain.Submission, error) {
	rows, err := s.q.ListSubmissions(ctx, ListSubmissionsParams{Column1: status, Limit: int32(limit)})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Submission, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainSubmission(r))
	}
	return out, nil
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

func (s *Store) ListEvents(ctx context.Context, limit, offset int) ([]domain.Event, error) {
	rows, err := s.q.ListEvents(ctx, ListEventsParams{Limit: int32(limit), Offset: int32(offset)})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Event, 0, len(rows))
	for _, r := range rows {
		var ref *uuid.UUID
		if r.Ref.Valid {
			id := toUUID(r.Ref)
			ref = &id
		}
		out = append(out, domain.Event{
			ID: toUUID(r.ID), Type: r.Type, Ref: ref,
			Payload: json.RawMessage(r.Payload), CreatedAt: r.CreatedAt.Time,
		})
	}
	return out, nil
}

func (s *Store) CountEvents(ctx context.Context) (int64, error) {
	return s.q.CountEvents(ctx)
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

func (s *Store) UpsertUser(ctx context.Context, in domain.User) (domain.User, error) {
	row, err := s.q.UpsertUser(ctx, UpsertUserParams{
		Subject: in.Subject,
		Upn:     in.UPN,
		Email:   in.Email,
		Name:    in.Name,
		Column5: in.Role,
	})
	if err != nil {
		return domain.User{}, err
	}
	return toDomainUser(row), nil
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := s.q.GetUserByID(ctx, toPgUUID(id))
	if err != nil {
		return domain.User{}, translate(err)
	}
	return toDomainUser(row), nil
}

func (s *Store) CreateSession(ctx context.Context, in domain.Session) (domain.Session, error) {
	row, err := s.q.CreateSession(ctx, CreateSessionParams{
		UserID:    toPgUUID(in.UserID),
		TokenHash: in.TokenHash,
		ExpiresAt: toPgTime(in.ExpiresAt),
	})
	if err != nil {
		return domain.Session{}, err
	}
	return toDomainSession(row), nil
}

func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error) {
	row, err := s.q.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return domain.Session{}, translate(err)
	}
	return toDomainSession(row), nil
}

func (s *Store) DeleteSession(ctx context.Context, id uuid.UUID) error {
	return s.q.DeleteSession(ctx, toPgUUID(id))
}

func (s *Store) EnqueueJob(ctx context.Context, kind domain.JobKind, ref uuid.UUID, runAt time.Time) error {
	return s.q.EnqueueJob(ctx, EnqueueJobParams{
		Kind:  string(kind),
		Ref:   toPgUUID(ref),
		RunAt: toPgTime(runAt),
	})
}

func (s *Store) ClaimDueJob(ctx context.Context) (*domain.Job, error) {
	row, err := s.q.ClaimDueJob(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j := toDomainJob(row)
	return &j, nil
}

func (s *Store) CompleteJob(ctx context.Context, id uuid.UUID) error {
	return s.q.CompleteJob(ctx, toPgUUID(id))
}

func (s *Store) FailJob(ctx context.Context, id uuid.UUID, reason string) error {
	return s.q.FailJob(ctx, FailJobParams{
		ID:        toPgUUID(id),
		LastError: reason,
	})
}

func (s *Store) RequeueJob(ctx context.Context, id uuid.UUID) error {
	_, err := s.q.RequeueJob(ctx, toPgUUID(id))
	return translate(err)
}

func (s *Store) RequeueStaleJobs(ctx context.Context) error {
	return s.q.RequeueStaleJobs(ctx)
}

func (s *Store) ListJobsByState(ctx context.Context, st domain.JobState, limit int) ([]domain.Job, error) {
	rows, err := s.q.ListJobsByState(ctx, ListJobsByStateParams{
		State: string(st),
		Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainJob(r))
	}
	return out, nil
}

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

func (s *Store) ListJobsByRef(ctx context.Context, ref uuid.UUID) ([]domain.Job, error) {
	rows, err := s.q.ListJobsByRef(ctx, toPgUUID(ref))
	if err != nil {
		return nil, err
	}
	out := make([]domain.Job, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomainJob(r))
	}
	return out, nil
}

// translate maps driver errors onto domain errors.
func translate(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func toDomainSubmission(s Submission) domain.Submission {
	return domain.Submission{
		ID:             toUUID(s.ID),
		Filename:       s.Filename,
		SourceChecksum: s.SourceChecksum,
		Status:         domain.SubmissionStatus(s.Status),
		FailedReason:   s.FailedReason,
		ReceivedAt:     s.ReceivedAt.Time,
	}
}

func toDomainArtifact(a Artifact) domain.Artifact {
	return domain.Artifact{
		ID:           toUUID(a.ID),
		SubmissionID: toUUID(a.SubmissionID),
		Kind:         domain.ArtifactKind(a.Kind),
		Checksum:     a.Checksum,
		State:        domain.ArtifactState(a.State),
	}
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

func toDomainUser(u User) domain.User {
	return domain.User{
		ID:        toUUID(u.ID),
		Subject:   u.Subject,
		UPN:       u.Upn,
		Email:     u.Email,
		Name:      u.Name,
		Role:      u.Role,
		LastLogin: u.LastLoginAt.Time,
		CreatedAt: u.CreatedAt.Time,
	}
}

func toDomainSession(s Session) domain.Session {
	return domain.Session{
		ID:        toUUID(s.ID),
		UserID:    toUUID(s.UserID),
		TokenHash: s.TokenHash,
		ExpiresAt: s.ExpiresAt.Time,
		CreatedAt: s.CreatedAt.Time,
	}
}

func toDomainHoldDetail(
	id, entryID, release pgtype.UUID, status, reason, trace, rdfi, name, acct string,
	amount int64, tran int32, eff pgtype.Date, submissionID pgtype.UUID, customer, filename string,
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

func toDomainJob(j Job) domain.Job {
	return domain.Job{
		ID:        toUUID(j.ID),
		Kind:      domain.JobKind(j.Kind),
		Ref:       toUUID(j.Ref),
		State:     domain.JobState(j.State),
		Failures:  int(j.Failures),
		LastError: j.LastError,
		RunAt:     j.RunAt.Time,
	}
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
