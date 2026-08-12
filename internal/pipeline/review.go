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
			if m.Status != domain.HoldPending && m.Status != domain.HoldAutoDeclined {
				continue
			}
			if err := tx.SetHoldStatus(ctx, m.ID, decision); err != nil {
				return err
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
