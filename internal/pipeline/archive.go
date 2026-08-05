package pipeline

import (
	"context"
	"errors"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// ArchiveSubmission retires the submission's original and fixed artifacts. It
// only runs once both the cleaned and (if present) release artifacts are
// published; the transmitted artifacts stay in the outgoing directory and are
// cleaned up by a separate process. The retained bytes are untouched here —
// prune handles the retention window.
func ArchiveSubmission(ctx context.Context, d Deps, submissionID uuid.UUID) error {
	orig, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactOriginal)
	if err != nil {
		return err
	}
	fixed, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactFixed)
	if err != nil {
		return err
	}
	if orig.State == domain.ArtifactArchived && fixed.State == domain.ArtifactArchived {
		return nil
	}

	if cleaned, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactCleaned); err == nil {
		if cleaned.State != domain.ArtifactPublished {
			return ErrNotReady
		}
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	arts, err := d.Store.ListArtifactsBySubmission(ctx, submissionID)
	if err != nil {
		return err
	}
	for _, a := range arts {
		// A release resolves to published (approved and shipped) or archived
		// (blocked by a declined hold); either is final for archiving.
		if a.Kind == domain.ArtifactRelease &&
			a.State != domain.ArtifactPublished && a.State != domain.ArtifactArchived {
			return ErrNotReady
		}
	}

	if err := d.Store.WithinTx(ctx, func(tx ports.Store) error {
		if err := tx.SetArtifactState(ctx, orig.ID, domain.ArtifactArchived); err != nil {
			return err
		}
		return tx.SetArtifactState(ctx, fixed.ID, domain.ArtifactArchived)
	}); err != nil {
		return err
	}
	return emit(ctx, d, EvSubmissionArchived, &submissionID, nil)
}
