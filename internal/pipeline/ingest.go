package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"
)

// Ingest scans the intake area and registers each new file as a submission. A
// file whose (filename, content checksum) was seen within the dedup window is
// a duplicate and is skipped. Each registered file gets its bytes stored
// content-addressed, an original artifact row, and a fix job.
func Ingest(ctx context.Context, d Deps, in ports.Input) (*IngestResult, error) {
	files, err := in.Scan(ctx)
	if err != nil {
		return nil, err
	}
	res := &IngestResult{}
	now := d.Clock.Now()

	for _, f := range files {
		sum := checksum.Bytes(f.Data)

		// Store the bytes first (content-addressed, idempotent), then check for
		// a duplicate and register the submission + original artifact + fix job
		// atomically under an advisory lock, so two overlapping runs cannot
		// both register the same intake file.
		if err := d.Files.Put(ctx, sum, f.Data); err != nil {
			return nil, err
		}

		var sub domain.Submission
		duplicate := false
		if err := d.Store.WithIngestLock(ctx, func(tx ports.Store) error {
			dup, err := isDuplicate(ctx, tx, f.Filename, sum, now, d.Policy.DedupWindowDays)
			if err != nil {
				return err
			}
			if dup {
				duplicate = true
				return nil
			}
			sub, err = tx.CreateSubmission(ctx, domain.Submission{
				Filename:       f.Filename,
				SourceChecksum: sum,
				Status:         domain.SubmissionReceived,
				ReceivedAt:     now,
			})
			if err != nil {
				return err
			}
			if _, err := tx.CreateArtifact(ctx, domain.Artifact{
				SubmissionID: sub.ID,
				Kind:         domain.ArtifactOriginal,
				Checksum:     sum,
				State:        domain.ArtifactStaged,
			}); err != nil {
				return err
			}
			return tx.EnqueueJob(ctx, domain.JobFixSubmission, sub.ID, now)
		}); err != nil {
			return nil, err
		}

		if duplicate {
			res.Skipped++
			if err := Emit(ctx, d, EvDedupSkipped, nil, nil); err != nil {
				return nil, err
			}
			if err := in.Remove(ctx, f.Filename); err != nil {
				return nil, fmt.Errorf("removing intake file %s: %w", f.Filename, err)
			}
			continue
		}

		if err := Emit(ctx, d, EvSubmissionCreated, &sub.ID, nil); err != nil {
			return nil, err
		}
		res.Ingested++

		// The intake file is consumed; the bytes live in the content store.
		if err := in.Remove(ctx, f.Filename); err != nil {
			return nil, fmt.Errorf("removing intake file %s: %w", f.Filename, err)
		}
	}
	return res, nil
}

// isDuplicate reports whether the same (filename, checksum) was registered
// within the dedup window. An identical re-submission outside the window is a
// fresh generation, not a duplicate.
func isDuplicate(ctx context.Context, st ports.Store, filename, sum string, now time.Time, windowDays int) (bool, error) {
	recent, err := st.FindSubmissionsBySourceChecksum(ctx, sum)
	if err != nil {
		return false, err
	}
	cutoff := now.AddDate(0, 0, -windowDays)
	for _, s := range recent {
		if s.Filename == filename && s.ReceivedAt.After(cutoff) {
			return true, nil
		}
	}
	return false, nil
}
