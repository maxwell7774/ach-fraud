// Package domain holds the entities and value types of the ACH fraud
// pipeline. It is pure: no database, filesystem, or transport imports, so the
// rules of the domain can be reasoned about and tested without infrastructure.
package domain

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrNotFound is returned by store lookups when the requested entity does not
// exist.
var ErrNotFound = errors.New("not found")

// Submission is one arriving file. The filename is metadata, not identity:
// multiple submissions may share a name, and identity is the row id plus the
// content checksum of the source bytes.
type Submission struct {
	ID             uuid.UUID        `json:"id"`
	Filename       string           `json:"filename"`
	SourceChecksum string           `json:"source_checksum"`
	Status         SubmissionStatus `json:"status"`
	FailedReason   string           `json:"failed_reason"`
	ReceivedAt     time.Time        `json:"received_at"`
}

// SubmissionStatus tracks how far ingestion got for a submission.
type SubmissionStatus string

const (
	SubmissionReceived SubmissionStatus = "received"
	SubmissionReady    SubmissionStatus = "ready"
	SubmissionFailed   SubmissionStatus = "failed"
	SubmissionArchived SubmissionStatus = "archived"
)

// Artifact is one content-addressed output of a submission. Each kind exists
// at most once per submission. State is tracked on the row, not implied by a
// directory, so a staged and an archived artifact are the same bytes.
type Artifact struct {
	ID           uuid.UUID     `json:"id"`
	SubmissionID uuid.UUID     `json:"submission_id"`
	Kind         ArtifactKind  `json:"kind"`
	Checksum     string        `json:"checksum"`
	State        ArtifactState `json:"state"`
}

// ArtifactKind identifies the variants a submission produces.
type ArtifactKind string

const (
	ArtifactOriginal ArtifactKind = "original"
	ArtifactFixed    ArtifactKind = "fixed"
	ArtifactCleaned  ArtifactKind = "cleaned"
	ArtifactRelease  ArtifactKind = "release"
)

// ArtifactState tracks the artifact lifecycle.
type ArtifactState string

const (
	ArtifactStaged    ArtifactState = "staged"
	ArtifactPublished ArtifactState = "published"
	ArtifactArchived  ArtifactState = "archived"
	ArtifactPruned    ArtifactState = "pruned"
)

// BatchHeader is one batch's control header, linked to its submission.
type BatchHeader struct {
	ID                 uuid.UUID  `json:"id"`
	SubmissionID       uuid.UUID  `json:"submission_id"`
	CustomerID         string     `json:"customer_id"`
	CompanyName        string     `json:"company_name"`
	CompanyDescription string     `json:"company_description"`
	EffectiveDate      *time.Time `json:"effective_date"`
	// Joined from the submission by the web reads.
	Filename string `json:"filename"`
}

// BatchEntry is one entry detail line within a batch. EffectiveDate and
// CustomerID come from the owning header (joined by the store) so the hold
// rules can apply same-day velocity per customer.
type BatchEntry struct {
	ID              uuid.UUID  `json:"id"`
	HeaderID        uuid.UUID  `json:"header_id"`
	Rdfi            string     `json:"rdfi"`
	ReceiverName    string     `json:"receiver_name"`
	ReceiverAccount string     `json:"receiver_account"`
	Amount          int64      `json:"amount"`
	TranCode        int        `json:"tran_code"`
	Trace           string     `json:"trace"`
	EffectiveDate   *time.Time `json:"effective_date"`
	CustomerID      string     `json:"customer_id"`
	// Joined from the submission by the web reads.
	Filename string `json:"filename"`
}

// HoldStatus is the review state of a hold.
type HoldStatus string

const (
	HoldPending      HoldStatus = "pending"
	HoldApproved     HoldStatus = "approved"
	HoldDeclined     HoldStatus = "declined"
	HoldAutoDeclined HoldStatus = "auto_declined"
)

// Hold is a flagged entry awaiting (or having received) review.
type Hold struct {
	ID                uuid.UUID  `json:"id"`
	SubmissionID      uuid.UUID  `json:"submission_id"`
	EntryID           uuid.UUID  `json:"entry_id"`
	Status            HoldStatus `json:"status"`
	Reason            string     `json:"reason"`
	ReleaseArtifactID *uuid.UUID `json:"release_artifact_id,omitempty"`
	// Joined batch-entry data, populated by store lookups.
	EntryTrace        string     `json:"entry_trace"`
	EntryRdfi         string     `json:"entry_rdfi"`
	EntryReceiverName string     `json:"entry_receiver_name"`
	EntryReceiverAcct string     `json:"entry_receiver_account"`
	EntryAmount       int64      `json:"entry_amount"`
	EntryTranCode     int        `json:"entry_tran_code"`
	EffectiveDate     *time.Time `json:"effective_date"`
	CreatedAt         time.Time  `json:"created_at"`
	// Joined header/submission data (populated by the web reads).
	CustomerID  string `json:"customer_id"`
	CompanyName string `json:"company_name"`
	Filename    string `json:"filename"`
}

// Review records an audit-trail entry for a hold decision.
type Review struct {
	ID        uuid.UUID `json:"id"`
	HoldID    uuid.UUID `json:"hold_id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

// Verification records the last automated check that a submission's artifact
// chain (original → fixed → cleaned → release) balances, so a file still shows
// it passed even after the artifact bytes are pruned by retention.
type Verification struct {
	SubmissionID uuid.UUID `json:"submission_id"`
	Verified     bool      `json:"verified"`
	Issues       string    `json:"issues"`
	CheckedAt    time.Time `json:"checked_at"`
}

// Recipient is one email alert recipient. AlertTypes lists the digest
// categories the recipient receives; only admins change these.
type Recipient struct {
	ID         uuid.UUID `json:"id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	Enabled    bool      `json:"enabled"`
	AlertTypes []string  `json:"alert_types"`
	CreatedAt  time.Time `json:"created_at"`
}

// Email alert categories, per recipient.
const (
	AlertPendingHolds   = "pending_holds"
	AlertVelocityLeaks  = "velocity_leaks"
	AlertReleaseBlocked = "release_blocked"
	AlertFailed         = "failed"
)

// User is an identity imported from the identity provider (Microsoft Entra).
// Subject is the stable provider object id; profile fields are refreshed at
// each login, while Role is managed locally.
type User struct {
	ID        uuid.UUID `json:"id"`
	Subject   string    `json:"subject"`
	UPN       string    `json:"upn"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	LastLogin time.Time `json:"last_login_at"`
	CreatedAt time.Time `json:"created_at"`
}

// User roles, canonical values stored on users.role. Least privilege is the
// default: unknown/new users are watchers. SuperAdmin is the only role that
// sees events and manages email alert recipients; Admin covers the rest of the
// admin surface (files, entries, headers, artifacts).
const (
	RoleWatcher    = "watcher"
	RoleProcessor  = "processor"
	RoleAdmin      = "admin"
	RoleSuperAdmin = "super_admin"
)

// CanReview reports whether the role may approve/decline holds.
func (u User) CanReview() bool {
	return u.Role == RoleProcessor || u.Role == RoleAdmin || u.Role == RoleSuperAdmin
}

// IsAdmin reports whether the role may see files, entries, headers, artifacts.
func (u User) IsAdmin() bool {
	return u.Role == RoleAdmin || u.Role == RoleSuperAdmin
}

// IsSuperAdmin reports whether the role may also see events and manage email
// alert recipients.
func (u User) IsSuperAdmin() bool {
	return u.Role == RoleSuperAdmin
}

// Session is a server-side login session bound to a user. TokenHash is the
// sha256 of the raw session token stored in the user's cookie; the raw token
// itself is never persisted.
type Session struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	TokenHash string    `json:"token_hash"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// JobKind is the unit of work the pipeline executes. A job is idempotent per
// (kind, ref).
type JobKind string

const (
	JobFixSubmission  JobKind = "fix_submission"
	JobImport         JobKind = "import"
	JobScreen         JobKind = "screen"
	JobProcess        JobKind = "process"
	JobPublishCleaned JobKind = "publish_cleaned"
	JobPublishRelease JobKind = "publish_release"
	JobArchive        JobKind = "archive"
)

// JobState tracks a job through the outbox lifecycle.
type JobState string

const (
	JobQueued     JobState = "queued"
	JobInProgress JobState = "in_progress"
	JobDone       JobState = "done"
	JobFailed     JobState = "failed"
)

// Job is one unit of work in the outbox.
type Job struct {
	ID        uuid.UUID `json:"id"`
	Kind      JobKind   `json:"kind"`
	Ref       uuid.UUID `json:"ref"`
	State     JobState  `json:"state"`
	Failures  int       `json:"failures"`
	LastError string    `json:"last_error"`
	RunAt     time.Time `json:"run_at"`
}

// Event is a domain occurrence worth logging, alerting on, or measuring.
type Event struct {
	ID        uuid.UUID       `json:"id"`
	Type      string          `json:"type"`
	Ref       *uuid.UUID      `json:"ref,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// VelocitySum is the summed same-day credit amount for a receiver
// account/RDFI/customer group across all ready submissions.
type VelocitySum struct {
	ReceiverAccount string     `json:"receiver_account"`
	Rdfi            string     `json:"rdfi"`
	EffectiveDate   *time.Time `json:"effective_date"`
	CustomerID      string     `json:"customer_id"`
	Total           int64      `json:"total"`
}

// HoldCombo is a receiver account/RDFI pair that appears in the submission
// being screened, with whether the full hold history (no expiry) already
// whitelisted it (HasApproved) or blacklisted it (HasDeclined). Pending and
// auto_declined holds count for neither.
type HoldCombo struct {
	Rdfi            string `json:"rdfi"`
	ReceiverAccount string `json:"receiver_account"`
	HasApproved     bool   `json:"has_approved"`
	HasDeclined     bool   `json:"has_declined"`
}

// Policy bundles the tunables that drive the hold rules. Thresholds are in
// cents.
type Policy struct {
	HoldDays           int
	HoldingRDFI        string
	HoldingAccount     string
	HoldSingleAmount   int64
	HoldVelocityAmount int64
	DedupWindowDays    int
}
