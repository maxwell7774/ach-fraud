package pipeline

import (
	"context"
	"fmt"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

// PublishCleaned sends the intercept file and marks it published. When no
// release exists (nothing was held), it advances straight to archive.
func PublishCleaned(ctx context.Context, d Deps, submissionID uuid.UUID) error {
	cleaned, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactCleaned)
	if err != nil {
		return err
	}
	if cleaned.State != domain.ArtifactPublished {
		if cleaned.State != domain.ArtifactStaged {
			return fmt.Errorf("cleaned artifact %s is %s, cannot publish", cleaned.ID, cleaned.State)
		}
		data, err := d.Files.Get(ctx, cleaned.Checksum)
		if err != nil {
			return err
		}
		if err := d.Sender.Send(ctx, cleaned, data); err != nil {
			return err
		}
		if err := d.Store.SetArtifactState(ctx, cleaned.ID, domain.ArtifactPublished); err != nil {
			return err
		}
		if err := Emit(ctx, d, EvInterceptPublished, &submissionID, nil); err != nil {
			return err
		}
	}
	return maybeEnqueueArchive(ctx, d, submissionID)
}

// PublishRelease sends one release artifact once every hold linked to it is
// approved. Each release artifact covers one velocity group, so a split ships
// together. While any linked hold is pending the job returns ErrNotReady and
// the dispatcher re-queues it.
func PublishRelease(ctx context.Context, d Deps, releaseID uuid.UUID) error {
	release, err := d.Store.GetArtifactByID(ctx, releaseID)
	if err != nil {
		return err
	}
	if release.Kind != domain.ArtifactRelease {
		return fmt.Errorf("artifact %s is %s, not a release", releaseID, release.Kind)
	}
	if release.State == domain.ArtifactPublished {
		return maybeEnqueueArchive(ctx, d, release.SubmissionID)
	}

	holds, err := d.Store.ListHoldsByReleaseArtifact(ctx, release.ID)
	if err != nil {
		return err
	}
	if len(holds) == 0 {
		return fmt.Errorf("release file has no linked holds")
	}
	for _, h := range holds {
		if h.Status == domain.HoldDeclined {
			// A declined hold is a terminal result, not an error: the release
			// never ships, the intercept stays at the holding account, and the
			// release artifact retires so the rest of the file can archive.
			if err := d.Store.SetArtifactState(ctx, release.ID, domain.ArtifactArchived); err != nil {
				return err
			}
			if err := Emit(ctx, d, EvReleaseBlocked, &release.ID, nil); err != nil {
				return err
			}
			return maybeEnqueueArchive(ctx, d, release.SubmissionID)
		}
		if h.Status != domain.HoldApproved {
			return ErrNotReady
		}
	}

	data, err := d.Files.Get(ctx, release.Checksum)
	if err != nil {
		return err
	}
	file, err := achp.Read(data, true)
	if err != nil {
		return err
	}
	if legs := achp.TotalEntries(file); legs != len(holds) {
		return fmt.Errorf("release file has %d legs but %d linked holds", legs, len(holds))
	}

	// Bring the cleaned intercept along.
	cleaned, err := d.Store.GetArtifactBySubmissionKind(ctx, release.SubmissionID, domain.ArtifactCleaned)
	if err != nil {
		return err
	}
	if cleaned.State != domain.ArtifactPublished {
		cdata, err := d.Files.Get(ctx, cleaned.Checksum)
		if err != nil {
			return err
		}
		if err := d.Sender.Send(ctx, cleaned, cdata); err != nil {
			return err
		}
		if err := d.Store.SetArtifactState(ctx, cleaned.ID, domain.ArtifactPublished); err != nil {
			return err
		}
	}

	if err := d.Sender.Send(ctx, release, data); err != nil {
		return err
	}
	if err := d.Store.SetArtifactState(ctx, release.ID, domain.ArtifactPublished); err != nil {
		return err
	}
	if err := Emit(ctx, d, EvReleasePublished, &release.SubmissionID, nil); err != nil {
		return err
	}
	return maybeEnqueueArchive(ctx, d, release.SubmissionID)
}

// maybeEnqueueArchive queues the archive job once the cleaned artifact is
// published and every release artifact (if any) is published or blocked
// (archived by publish_release when a linked hold was declined).
func maybeEnqueueArchive(ctx context.Context, d Deps, submissionID uuid.UUID) error {
	cleaned, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactCleaned)
	if err != nil {
		return err
	}
	if cleaned.State != domain.ArtifactPublished {
		return nil
	}
	arts, err := d.Store.ListArtifactsBySubmission(ctx, submissionID)
	if err != nil {
		return err
	}
	for _, a := range arts {
		if a.Kind == domain.ArtifactRelease &&
			a.State != domain.ArtifactPublished && a.State != domain.ArtifactArchived {
			return nil
		}
	}
	return d.Store.EnqueueJob(ctx, domain.JobArchive, submissionID, d.Clock.Now())
}
