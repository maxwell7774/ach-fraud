package pipeline

import (
	"context"
	"errors"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"

	"github.com/google/uuid"
)

// FixSubmission repairs the submission's original bytes and registers the fixed
// artifact. An unparseable file fails the submission and surfaces as a job
// failure instead of being parked in an error directory.
func FixSubmission(ctx context.Context, d Deps, submissionID uuid.UUID) error {
	orig, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactOriginal)
	if err != nil {
		return err
	}

	// Idempotent: a fixed artifact already on file means the fix is done.
	if _, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactFixed); err == nil {
		return d.Store.EnqueueJob(ctx, domain.JobImport, submissionID, d.Clock.Now())
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}

	data, err := d.Files.Get(ctx, orig.Checksum)
	if err != nil {
		return err
	}
	file, err := achp.Read(data, true)
	if err != nil {
		if serr := d.Store.SetSubmissionStatus(ctx, submissionID, domain.SubmissionFailed, err.Error()); serr != nil {
			return serr
		}
		if eerr := Emit(ctx, d, EvSubmissionFailed, &submissionID, nil); eerr != nil {
			return eerr
		}
		return err
	}

	fixed, err := achp.Rebuild(file)
	if err != nil {
		return err
	}
	fixedData, err := achp.Write(fixed)
	if err != nil {
		return err
	}
	fixedSum := checksum.Bytes(fixedData)
	if err := d.Store.WithArtifactLifecycleLock(ctx, func(tx ports.Store) error {
		if err := d.Files.Put(ctx, fixedSum, fixedData); err != nil {
			return err
		}
		_, err := tx.CreateArtifact(ctx, domain.Artifact{
			SubmissionID: submissionID,
			Kind:         domain.ArtifactFixed,
			Checksum:     fixedSum,
			State:        domain.ArtifactStaged,
		})
		return err
	}); err != nil {
		return err
	}
	return d.Store.EnqueueJob(ctx, domain.JobImport, submissionID, d.Clock.Now())
}

// ImportSubmission loads the fixed file's batch headers and entries for the
// submission and marks it ready. Runs in a single transaction; a retry after a
// successful import is a no-op.
func ImportSubmission(ctx context.Context, d Deps, submissionID uuid.UUID) error {
	sub, err := d.Store.GetSubmission(ctx, submissionID)
	if err != nil {
		return err
	}
	if sub.Status == domain.SubmissionReady {
		return d.Store.EnqueueJob(ctx, domain.JobScreen, submissionID, d.Clock.Now())
	}

	fixed, err := d.Store.GetArtifactBySubmissionKind(ctx, submissionID, domain.ArtifactFixed)
	if err != nil {
		return err
	}
	data, err := d.Files.Get(ctx, fixed.Checksum)
	if err != nil {
		return err
	}
	file, err := achp.Read(data, true)
	if err != nil {
		return err
	}
	batches, err := achp.ParseBatches(file)
	if err != nil {
		return err
	}

	if err := d.Store.WithinTx(ctx, func(tx ports.Store) error {
		for _, b := range batches {
			hdr, err := tx.CreateBatchHeader(ctx, domain.BatchHeader{
				SubmissionID:       submissionID,
				CustomerID:         b.Header.CustomerID,
				CompanyName:        b.Header.CompanyName,
				CompanyDescription: b.Header.CompanyDescription,
				EffectiveDate:      b.Header.EffectiveDate,
			})
			if err != nil {
				return err
			}
			for _, e := range b.Entries {
				e.HeaderID = hdr.ID
				if _, err := tx.CreateBatchEntry(ctx, e); err != nil {
					return err
				}
			}
		}
		return tx.SetSubmissionStatus(ctx, submissionID, domain.SubmissionReady, "")
	}); err != nil {
		return err
	}
	return d.Store.EnqueueJob(ctx, domain.JobScreen, submissionID, d.Clock.Now())
}
