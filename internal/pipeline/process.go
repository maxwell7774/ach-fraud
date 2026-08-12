package pipeline

import (
	"context"
	"errors"
	"fmt"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// ProcessSubmission builds the cleaned and release artifacts for a screened
// submission, verifies the money balances across all three files, registers the
// children, and enqueues the publish jobs. It is idempotent: a submission with
// an existing cleaned artifact is already processed.
func ProcessSubmission(ctx context.Context, d Deps, submissionID uuid.UUID) (*ProcessResult, error) {
	// Already processed (a retry after a partial run).
	if _, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactCleaned); err == nil {
		if err := d.Store.EnqueueJob(ctx, domain.JobPublishCleaned, submissionID, d.Clock.Now()); err != nil {
			return nil, err
		}
		if _, rerr := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactRelease); rerr == nil {
			if err := d.Store.EnqueueJob(ctx, domain.JobPublishRelease, submissionID, d.Clock.Now()); err != nil {
				return nil, err
			}
		} else if !errors.Is(rerr, domain.ErrNotFound) {
			return nil, rerr
		}
		return nil, nil
	} else if !errors.Is(err, domain.ErrNotFound) {
		return nil, err
	}

	fixed, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactFixed)
	if err != nil {
		return nil, err
	}
	orig, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactOriginal)
	if err != nil {
		return nil, err
	}

	fixedData, err := d.Files.Get(ctx, fixed.Checksum)
	if err != nil {
		return nil, err
	}
	origData, err := d.Files.Get(ctx, orig.Checksum)
	if err != nil {
		return nil, err
	}

	fixedFile, err := achp.Read(fixedData, true)
	if err != nil {
		return nil, err
	}
	origFile, err := achp.Read(origData, true)
	if err != nil {
		return nil, err
	}
	if err := achp.VerifySame(origFile, fixedFile); err != nil {
		return nil, fmt.Errorf("fixed file differs from original: %w", err)
	}

	holds, err := d.Store.ListHoldsBySubmission(ctx, submissionID)
	if err != nil {
		return nil, err
	}
	held, err := achp.MatchHolds(fixedFile, holds)
	if err != nil {
		return nil, err
	}

	cleaned, err := achp.BuildCleaned(fixedFile, held, d.Policy)
	if err != nil {
		return nil, err
	}

	// Group pending/approved holds into release units: one release file per
	// same-day velocity group. A velocity split ships together once the account
	// is approved, while unrelated holds release independently. Each group gets
	// its own release artifact and its own publish job.
	groups := releaseGroups(held)

	// Verify the cleaned file against ALL held entries once (release=nil skips
	// the release-leg check, which runs per group below).
	if err := achp.VerifyConsistency(fixedFile, cleaned, nil, held, d.Policy); err != nil {
		return nil, fmt.Errorf("balance check failed: %w", err)
	}

	cleanedData, err := achp.Write(cleaned)
	if err != nil {
		return nil, err
	}
	cleanedSum := checksum.Bytes(cleanedData)
	if err := d.Files.Put(ctx, cleanedSum, cleanedData); err != nil {
		return nil, err
	}

	type builtRelease struct {
		sum  string
		ids  []uuid.UUID
		held []achp.HeldEntry
	}
	releaseDate := d.Clock.Now().Format("060102")
	var releases []builtRelease
	for _, g := range groups {
		rel, _, ids, err := achp.BuildRelease(fixedFile, g.held, d.Policy, releaseDate)
		if err != nil {
			return nil, err
		}
		if rel == nil {
			continue
		}
		data, err := achp.Write(rel)
		if err != nil {
			return nil, err
		}
		sum := checksum.Bytes(data)
		if err := d.Files.Put(ctx, sum, data); err != nil {
			return nil, err
		}
		if err := achp.VerifyRelease(rel, g.held, d.Policy); err != nil {
			return nil, fmt.Errorf("release balance check failed: %w", err)
		}
		releases = append(releases, builtRelease{sum: sum, ids: ids, held: g.held})
	}

	// Re-read what was actually written and re-verify, so a serialization
	// round-trip bug cannot slip past with DB rows already committed.
	cleanedOnDisk, err := readFile(ctx, d.Files, cleanedSum)
	if err != nil {
		return nil, err
	}
	if err := achp.VerifyConsistency(fixedFile, cleanedOnDisk, nil, held, d.Policy); err != nil {
		return nil, fmt.Errorf("balance check failed after writing: %w", err)
	}
	for _, r := range releases {
		rel, err := readFile(ctx, d.Files, r.sum)
		if err != nil {
			return nil, err
		}
		if err := achp.VerifyRelease(rel, r.held, d.Policy); err != nil {
			return nil, fmt.Errorf("release balance check failed after writing: %w", err)
		}
	}

	var relIDs []uuid.UUID
	if err := d.Store.WithinTx(ctx, func(tx ports.Store) error {
		if _, err := tx.CreateArtifact(ctx, domain.Artifact{
			SubmissionID: submissionID,
			Kind:         domain.ArtifactCleaned,
			Checksum:     cleanedSum,
			State:        domain.ArtifactStaged,
		}); err != nil {
			return err
		}
		for _, r := range releases {
			rel, err := tx.CreateArtifact(ctx, domain.Artifact{
				SubmissionID: submissionID,
				Kind:         domain.ArtifactRelease,
				Checksum:     r.sum,
				State:        domain.ArtifactStaged,
			})
			if err != nil {
				return err
			}
			for _, hid := range r.ids {
				if err := tx.SetHoldReleaseArtifact(ctx, hid, rel.ID); err != nil {
					return err
				}
			}
			relIDs = append(relIDs, rel.ID)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	now := d.Clock.Now()
	if err := d.Store.UpsertVerification(ctx, submissionID, true, ""); err != nil {
		return nil, err
	}
	if err := d.Store.EnqueueJob(ctx, domain.JobPublishCleaned, submissionID, now); err != nil {
		return nil, err
	}
	for _, id := range relIDs {
		if err := d.Store.EnqueueJob(ctx, domain.JobPublishRelease, id, now); err != nil {
			return nil, err
		}
	}

	released := 0
	for _, r := range releases {
		released += len(r.held)
	}
	return &ProcessResult{
		Entries:    achp.TotalEntries(fixedFile),
		Held:       len(held),
		Released:   released,
		HasRelease: len(releases) > 0,
	}, nil
}

type releaseGroup struct {
	held []achp.HeldEntry
}

// releaseGroups splits held entries into release units by their same-day
// velocity group, preserving deterministic order.
func releaseGroups(held []achp.HeldEntry) []*releaseGroup {
	byKey := map[string]*releaseGroup{}
	var order []string
	for _, hr := range held {
		if hr.Hold.Status != domain.HoldPending && hr.Hold.Status != domain.HoldApproved {
			continue
		}
		key := releaseGroupKey(hr.Hold)
		g, ok := byKey[key]
		if !ok {
			g = &releaseGroup{}
			byKey[key] = g
			order = append(order, key)
		}
		g.held = append(g.held, hr)
	}
	out := make([]*releaseGroup, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

func releaseGroupKey(h domain.Hold) string {
	day := ""
	if h.EffectiveDate != nil {
		day = h.EffectiveDate.Format("2006-01-02")
	}
	return h.EntryReceiverAcct + "|" + h.EntryRdfi + "|" + day + "|" + h.CustomerID
}
