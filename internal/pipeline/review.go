package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// ApproveHold approves a pending (or auto-declined) hold, records an audit
// review, and re-queues the release publish so an approved release ships
// immediately rather than waiting for the next poll.
//
// A hold that is part of a velocity group is decided for the WHOLE group:
// every hold sharing its release artifact is approved too, because a split is
// only ever approved or declined as a unit.
func ApproveHold(ctx context.Context, d Deps, holdID uuid.UUID, actor, note string) error {
	return decide(ctx, d, holdID, actor, note, domain.HoldApproved)
}

// DeclineHold declines a pending (or auto-declined) hold and records the
// review. Declining never ships anything; a declined hold blocks its velocity
// group's release from publishing.
func DeclineHold(ctx context.Context, d Deps, holdID uuid.UUID, actor, note string) error {
	return decide(ctx, d, holdID, actor, note, domain.HoldDeclined)
}

// decide applies one decision (approve or decline) to a hold and, if it
// belongs to a velocity group, to every hold in that group. All members are
// decided in a single transaction and the group's release publish is re-queued.
func decide(ctx context.Context, d Deps, holdID uuid.UUID, actor, note string, decision domain.HoldStatus) error {
	h, err := d.Store.GetHold(ctx, holdID)
	if err != nil {
		return err
	}
	if h.Status != domain.HoldPending && h.Status != domain.HoldAutoDeclined {
		return fmt.Errorf("%w: hold %s is %s", ErrAlreadyReviewed, holdID, h.Status)
	}

	// A velocity group's holds share a release artifact; decide them as a unit.
	// A hold whose release has a single leg decides alone.
	group := []domain.Hold{h}
	if h.ReleaseArtifactID != nil {
		members, err := d.Store.ListHoldsByReleaseArtifact(ctx, *h.ReleaseArtifactID)
		if err != nil {
			return err
		}
		if len(members) > 1 {
			group = members
		}
	}

	action := "approved"
	ev := EvHoldApproved
	if decision == domain.HoldDeclined {
		action = "declined"
		ev = EvHoldDeclined
	}

	var decided []uuid.UUID
	if err := d.Store.WithinTx(ctx, func(tx ports.Store) error {
		for _, m := range group {
			changed, err := tx.SetHoldStatusIfOpen(ctx, m.ID, decision)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			if err := tx.CreateReview(ctx, domain.Review{HoldID: m.ID, Actor: actor, Action: action, Note: note}); err != nil {
				return err
			}
			decided = append(decided, m.ID)
		}
		return nil
	}); err != nil {
		return err
	}
	if len(decided) == 0 {
		return fmt.Errorf("%w: hold %s was already reviewed", ErrAlreadyReviewed, holdID)
	}
	for _, id := range decided {
		if err := Emit(ctx, d, ev, &id, nil); err != nil {
			return err
		}
	}

	// Re-run this group's release gate now so an approved group ships
	// immediately and a declined one resolves as blocked.
	if h.ReleaseArtifactID != nil {
		return d.Store.EnqueueJob(ctx, domain.JobPublishRelease, *h.ReleaseArtifactID, d.Clock.Now())
	}
	return nil
}

// ApproveHolds reviews several holds in bulk, skipping any already decided, and
// returns how many were approved.
func ApproveHolds(ctx context.Context, d Deps, ids []uuid.UUID, actor, note string) (int, error) {
	return bulkReview(ctx, d, ids, actor, note, ApproveHold)
}

// DeclineHolds reviews several holds in bulk, skipping any already decided, and
// returns how many were declined.
func DeclineHolds(ctx context.Context, d Deps, ids []uuid.UUID, actor, note string) (int, error) {
	return bulkReview(ctx, d, ids, actor, note, DeclineHold)
}

func bulkReview(ctx context.Context, d Deps, ids []uuid.UUID, actor, note string, fn func(context.Context, Deps, uuid.UUID, string, string) error) (int, error) {
	count := 0
	for _, id := range ids {
		if err := fn(ctx, d, id, actor, note); err != nil {
			if errors.Is(err, ErrAlreadyReviewed) {
				continue
			}
			return count, err
		}
		count++
	}
	return count, nil
}

// ErrInvalidHoldStatus is returned when an override targets a status that is
// not one of the supported hold states.
var ErrInvalidHoldStatus = errors.New("invalid hold status")

// overrideableStatus reports whether a hold may be overridden to the given
// status. Every terminal state (and a reset to pending) is allowed; the
// behavior is purely a record change for future screening, so any of the
// existing states is fair game.
func overrideableStatus(s domain.HoldStatus) bool {
	switch s {
	case domain.HoldPending, domain.HoldApproved, domain.HoldDeclined, domain.HoldAutoDeclined:
		return true
	}
	return false
}

// OverrideHoldStatus changes a hold's status directly (e.g. approved to
// declined, or declined to approved) for record-keeping and future screening.
// Unlike ApproveHold/DeclineHold it does NOT re-run the file pipeline or
// re-queue release jobs — the money already moved keeps its outcome, and only
// the combo's standing for later files changes. When group is true and the
// hold belongs to a velocity group, every hold sharing its release artifact is
// overridden together, because a split's standing is decided as a unit. Each
// change is audited with a review.
func OverrideHoldStatus(ctx context.Context, d Deps, holdID uuid.UUID, actor, note string, status domain.HoldStatus, group bool) (int, error) {
	if !overrideableStatus(status) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidHoldStatus, status)
	}
	h, err := d.Store.GetHold(ctx, holdID)
	if err != nil {
		return 0, err
	}

	// A velocity group's holds share a release artifact; override them as a
	// unit (when requested) so the combo's standing is consistent.
	targets := []domain.Hold{h}
	if group && h.ReleaseArtifactID != nil {
		members, err := d.Store.ListHoldsByReleaseArtifact(ctx, *h.ReleaseArtifactID)
		if err != nil {
			return 0, err
		}
		if len(members) > 1 {
			targets = members
		}
	}

	action := string(status)
	if err := d.Store.WithinTx(ctx, func(tx ports.Store) error {
		for _, m := range targets {
			if err := tx.SetHoldStatus(ctx, m.ID, status); err != nil {
				return err
			}
			if err := tx.CreateReview(ctx, domain.Review{HoldID: m.ID, Actor: actor, Action: action, Note: note}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return len(targets), nil
}

// OverrideHolds changes several holds to the same status in bulk, skipping any
// that fail, and returns how many were changed.
func OverrideHolds(ctx context.Context, d Deps, ids []uuid.UUID, actor, note string, status domain.HoldStatus) (int, error) {
	if !overrideableStatus(status) {
		return 0, fmt.Errorf("%w: %q", ErrInvalidHoldStatus, status)
	}
	count := 0
	for _, id := range ids {
		n, err := OverrideHoldStatus(ctx, d, id, actor, note, status, true)
		if err != nil {
			return count, err
		}
		count += n
	}
	return count, nil
}

// ManuallyCreateHold creates a hold on a specific entry outside of the normal
// screening flow — e.g. an analyst flagging an entry for review, or manually
// whitelisting/blacklisting a receiver. It does NOT re-run the file pipeline or
// touch any transmitted file; the new hold only affects how future files to
// that receiver are screened (via the last-updated combo standing) and is
// recorded with a review for audit. Only approved and declined statuses are
// allowed; pending and auto_declined are system statuses from the screening
// pipeline.
func ManuallyCreateHold(ctx context.Context, d Deps, entryID uuid.UUID, actor, reason string, status domain.HoldStatus) (uuid.UUID, error) {
	if status != domain.HoldApproved && status != domain.HoldDeclined {
		return uuid.Nil, fmt.Errorf("%w: %q (manual holds can only be approved or declined)", ErrInvalidHoldStatus, status)
	}
	if _, err := d.Store.GetEntryByID(ctx, entryID); err != nil {
		return uuid.Nil, err
	}
	h, err := d.Store.CreateHold(ctx, entryID, status, reason)
	if err != nil {
		return uuid.Nil, err
	}
	if err := d.Store.CreateReview(ctx, domain.Review{HoldID: h.ID, Actor: actor, Action: string(status), Note: reason}); err != nil {
		return uuid.Nil, err
	}
	return h.ID, nil
}
