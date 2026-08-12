// Package pipeline implements the ACH fraud stages as functions over ports.
// Every stage takes a Deps bundle of interfaces — never a concrete store or
// filesystem — so the whole pipeline is testable with fakes and can run under
// a CLI, a scheduler, or a daemon without changes.
package pipeline

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// ErrNotReady marks a stage that cannot complete yet (for example a release
// whose linked holds are not all approved). The job dispatcher re-queues the
// job instead of marking it done or failed.
var ErrNotReady = errors.New("not ready yet")

// ErrAlreadyReviewed is returned when a review action targets a hold that has
// already been decided.
var ErrAlreadyReviewed = errors.New("already reviewed")

// Deps bundles the boundaries a stage may use. It is intentionally small: the
// stages have no idea what database, filesystem, transport, or clock is behind
// the interfaces.
type Deps struct {
	Store    ports.Store
	Files    ports.Files
	Sender   ports.Sender
	Notifier ports.Notifier
	Clock    ports.Clock
	Policy   domain.Policy
}

// Event types recorded by the pipeline.
const (
	EvSubmissionCreated  = "submission_created"
	EvSubmissionFailed   = "submission_failed"
	EvDedupSkipped       = "dedup_skipped"
	EvHoldCreated        = "hold_created"
	EvHoldAutoDeclined   = "hold_auto_declined"
	EvVelocityCrossed    = "velocity_crossed"
	EvInterceptPublished = "intercept_published"
	EvReleasePublished   = "release_published"
	EvReleaseBlocked     = "release_blocked"
	EvSubmissionArchived = "submission_archived"
	EvHoldApproved       = "hold_approved"
	EvHoldDeclined       = "hold_declined"
	EvSubmissionPruned   = "submission_pruned"
	EvJobFailed          = "job_failed"
)

// Emit records an event in the store and hands it to the notifier.
func Emit(ctx context.Context, d Deps, typ string, ref *uuid.UUID, payload json.RawMessage) error {
	if err := d.Store.AppendEvent(ctx, typ, ref, payload); err != nil {
		return err
	}
	return d.Notifier.Notify(ctx, domain.Event{Type: typ, Ref: ref, Payload: payload})
}

// IngestResult summarizes one intake pass.
type IngestResult struct {
	Ingested int
	Skipped  int
}

// ProcessResult summarizes one process run.
type ProcessResult struct {
	Entries    int
	Held       int
	Released   int
	HasRelease bool
}

// PruneResult summarizes one retention pass.
type PruneResult struct {
	Deleted int
	Rows    int
}
