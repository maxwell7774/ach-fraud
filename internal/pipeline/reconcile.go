package pipeline

import (
	"context"

	"github.com/27actions/ach/internal/domain"
)

// ReconcileReceived re-queues the fix job for any received submission that has
// none. These are submissions from a run that died between registering the
// submission and enqueueing its first job; the intake file is already consumed,
// so this sweep is the only thing that can re-drive them.
func ReconcileReceived(ctx context.Context, d Deps) (int, error) {
	orphans, err := d.Store.ListReceivedWithoutFixJob(ctx)
	if err != nil {
		return 0, err
	}
	for _, s := range orphans {
		if err := d.Store.EnqueueJob(ctx, domain.JobFixSubmission, s.ID, d.Clock.Now()); err != nil {
			return 0, err
		}
	}
	return len(orphans), nil
}
