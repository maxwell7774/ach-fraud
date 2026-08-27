package pipeline

import (
	"context"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/ports"
)

// Prune retires artifacts older than retentionDays and deletes their bytes once
// no remaining artifact references the same checksum. Because the store is
// content-addressed, bytes are only removed when every artifact that shares the
// checksum has been pruned, so newer generations of the same file are never
// touched.
func Prune(ctx context.Context, d Deps, retentionDays int) (*PruneResult, error) {
	res := &PruneResult{}
	cutoff := d.Clock.Now().AddDate(0, 0, -retentionDays)

	for _, state := range []domain.ArtifactState{domain.ArtifactArchived, domain.ArtifactPublished} {
		var candidates []domain.Artifact
		var doomed []string
		if err := d.Store.WithArtifactLifecycleLock(ctx, func(tx ports.Store) error {
			var err error
			candidates, err = tx.ListArtifactsByStateOlderThan(ctx, state, cutoff)
			if err != nil || len(candidates) == 0 {
				return err
			}
			for _, c := range candidates {
				if err := tx.SetArtifactState(ctx, c.ID, domain.ArtifactPruned); err != nil {
					return err
				}
			}
			doomedSet := map[string]bool{}
			for _, c := range candidates {
				if doomedSet[c.Checksum] {
					continue
				}
				refs, err := tx.ListArtifactsByChecksum(ctx, c.Checksum)
				if err != nil {
					return err
				}
				allPruned := len(refs) > 0
				for _, r := range refs {
					if r.State != domain.ArtifactPruned {
						allPruned = false
						break
					}
				}
				if allPruned {
					doomed = append(doomed, c.Checksum)
					doomedSet[c.Checksum] = true
				}
			}
			for _, sum := range doomed {
				if err := d.Files.Delete(ctx, sum); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}

		res.Deleted += len(doomed)
		res.Rows += len(candidates)
	}
	return res, nil
}
