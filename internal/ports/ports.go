// Package ports defines the boundaries between the pipeline's business rules
// and the outside world. The pipeline depends only on these interfaces; the
// concrete adapters (pgstore, localfiles, sender, notifier) implement them.
// Nothing outside the adapters may import a database driver or touch the
// filesystem directly.
package ports

import (
	"context"
	"encoding/json"
	"time"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

// Store is the persistence boundary for the whole pipeline.
type Store interface {
	// WithinTx runs fn inside a transaction, passing a Store bound to that
	// transaction so the pipeline can make multi-row writes atomic.
	WithinTx(ctx context.Context, fn func(tx Store) error) error

	// WithIngestLock runs fn inside a transaction that holds a global advisory
	// lock, serializing the ingest pass across processes so two overlapping
	// runs cannot both register the same intake file.
	WithIngestLock(ctx context.Context, fn func(tx Store) error) error
	// WithArtifactLifecycleLock serializes artifact byte creation/registration
	// with retention. The callback receives the transaction holding the lock.
	WithArtifactLifecycleLock(ctx context.Context, fn func(tx Store) error) error

	// Submissions
	CreateSubmission(ctx context.Context, s domain.Submission) (domain.Submission, error)
	GetSubmission(ctx context.Context, id uuid.UUID) (domain.Submission, error)
	FindSubmissionsBySourceChecksum(ctx context.Context, checksum string) ([]domain.Submission, error)
	SetSubmissionStatus(ctx context.Context, id uuid.UUID, st domain.SubmissionStatus, reason string) error
	ListReceivedWithoutFixJob(ctx context.Context) ([]domain.Submission, error)

	// Artifacts
	CreateArtifact(ctx context.Context, a domain.Artifact) (domain.Artifact, error)
	GetArtifactBySubmissionKind(ctx context.Context, submissionID uuid.UUID, kind domain.ArtifactKind) (domain.Artifact, error)
	GetArtifactByID(ctx context.Context, id uuid.UUID) (domain.Artifact, error)
	ListArtifactsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Artifact, error)
	SetArtifactState(ctx context.Context, id uuid.UUID, st domain.ArtifactState) error
	SetArtifactTotals(ctx context.Context, id uuid.UUID, debitTotal, creditTotal int64, debitEntries, creditEntries int) error

	// Batches
	CreateBatchHeader(ctx context.Context, h domain.BatchHeader) (domain.BatchHeader, error)
	CreateBatchEntry(ctx context.Context, e domain.BatchEntry) (domain.BatchEntry, error)
	ListEntriesBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.BatchEntry, error)
	HasEntryByRdfiAccount(ctx context.Context, rdfi, account string) (bool, error)
	GetEntryByID(ctx context.Context, entryID uuid.UUID) (domain.BatchEntry, error)
	SumVelocity(ctx context.Context, cutoff time.Time, submissionID uuid.UUID) ([]domain.VelocitySum, error)
	SumHeldByGroup(ctx context.Context, cutoff time.Time, submissionID uuid.UUID) ([]domain.VelocitySum, error)
	ListEntriesFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.BatchEntry, error)
	CountEntriesFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error)
	ListHeadersFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.BatchHeader, error)
	CountHeadersFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error)

	// Holds
	CreateHold(ctx context.Context, entryID uuid.UUID, status domain.HoldStatus, reason string) (domain.Hold, error)
	ListHoldsBySubmission(ctx context.Context, submissionID uuid.UUID) ([]domain.Hold, error)
	ListHoldsByReleaseArtifact(ctx context.Context, artifactID uuid.UUID) ([]domain.Hold, error)
	ListAllHolds(ctx context.Context) ([]domain.Hold, error)
	ListHoldsByStatus(ctx context.Context, status string, limit int) ([]domain.Hold, error)
	ListHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Hold, error)
	CountHoldsFiltered(ctx context.Context, status, search string, start, end *time.Time) (int64, error)
	CountHoldsByStatus(ctx context.Context, cutoff time.Time) (map[string]int64, error)
	GetHold(ctx context.Context, id uuid.UUID) (domain.Hold, error)
	ListCombosBySubmission(ctx context.Context, submissionID uuid.UUID, cutoff time.Time) ([]domain.HoldCombo, error)
	SetHoldStatus(ctx context.Context, id uuid.UUID, status domain.HoldStatus) error
	SetHoldStatusIfOpen(ctx context.Context, id uuid.UUID, status domain.HoldStatus) (bool, error)
	SetHoldReleaseArtifact(ctx context.Context, holdID, artifactID uuid.UUID) error

	// Reviews
	CreateReview(ctx context.Context, r domain.Review) error
	ListReviewsByHold(ctx context.Context, holdID uuid.UUID) ([]domain.Review, error)

	// Verification
	UpsertVerification(ctx context.Context, submissionID uuid.UUID, verified bool, issues string) error
	GetVerification(ctx context.Context, submissionID uuid.UUID) (domain.Verification, error)

	// Recipients (email alert subscriptions, admin-managed)
	ListRecipients(ctx context.Context) ([]domain.Recipient, error)
	CreateRecipient(ctx context.Context, r domain.Recipient) (domain.Recipient, error)
	UpdateRecipient(ctx context.Context, r domain.Recipient) (domain.Recipient, error)
	DeleteRecipient(ctx context.Context, id uuid.UUID) error

	// Auth (users + sessions)
	UpsertUser(ctx context.Context, u domain.User) (domain.User, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	CreateSession(ctx context.Context, s domain.Session) (domain.Session, error)
	GetSessionByTokenHash(ctx context.Context, tokenHash string) (domain.Session, error)
	DeleteSession(ctx context.Context, id uuid.UUID) error

	// Submissions
	ListSubmissions(ctx context.Context, status string, limit int) ([]domain.Submission, error)
	ListSubmissionsFiltered(ctx context.Context, status, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Submission, error)
	CountSubmissionsFiltered(ctx context.Context, status, search string, start, end *time.Time) (int64, error)

	// Retention
	ListArtifactsByStateOlderThan(ctx context.Context, state domain.ArtifactState, cutoff time.Time) ([]domain.Artifact, error)
	ListArtifactsByChecksum(ctx context.Context, checksum string) ([]domain.Artifact, error)

	// Jobs (outbox)
	EnqueueJob(ctx context.Context, kind domain.JobKind, ref uuid.UUID, runAt time.Time) error
	ClaimDueJob(ctx context.Context) (*domain.Job, error)
	CompleteJob(ctx context.Context, id uuid.UUID) error
	FailJob(ctx context.Context, id uuid.UUID, reason string) error
	RequeueJob(ctx context.Context, id uuid.UUID) error
	RequeueStaleJobs(ctx context.Context) error
	ListJobsByState(ctx context.Context, st domain.JobState, limit int) ([]domain.Job, error)
	ListJobsByRef(ctx context.Context, ref uuid.UUID) ([]domain.Job, error)

	// Events
	AppendEvent(ctx context.Context, typ string, ref *uuid.UUID, payload json.RawMessage) error
	ListEvents(ctx context.Context, limit, offset int) ([]domain.Event, error)
	ListEventsFiltered(ctx context.Context, search string, start, end *time.Time, sort, dir string, limit, offset int) ([]domain.Event, error)
	CountEventsFiltered(ctx context.Context, search string, start, end *time.Time) (int64, error)
}

// Files is a content-addressed byte store for artifacts. The checksum is the
// key, so the same bytes are stored once no matter how many artifacts reference
// them; lifecycle is tracked on artifact rows in the Store.
type Files interface {
	Get(ctx context.Context, checksum string) ([]byte, error)
	Put(ctx context.Context, checksum string, data []byte) error
	Delete(ctx context.Context, checksum string) error
}

// Sender transmits a published artifact to the outside world (today: the
// outgoing directory; later: SFTP, S3, or an upstream gateway).
type Sender interface {
	Send(ctx context.Context, a domain.Artifact, data []byte) error
}

// InputFile is a file waiting in the intake area.
type InputFile struct {
	Filename string
	Data     []byte
}

// Input is the intake boundary: where raw ACH files arrive before they are
// registered as submissions.
type Input interface {
	Scan(ctx context.Context) ([]InputFile, error)
	Remove(ctx context.Context, filename string) error
}

// Clock abstracts time so pipeline logic is deterministic in tests.
type Clock interface {
	Now() time.Time
}

// Notifier is the alerting boundary. The no-op adapter is used until real
// channels (SMTP, webhooks) are wired up. Notify receives events as the
// pipeline runs; Flush is called once at the end of a run so an adapter can
// aggregate a digest instead of emailing per event.
type Notifier interface {
	Notify(ctx context.Context, e domain.Event) error
	Flush(ctx context.Context) error
}
