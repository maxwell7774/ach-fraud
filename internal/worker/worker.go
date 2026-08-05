// Package worker drives the pipeline by claiming and dispatching jobs from the
// outbox. It is transport-agnostic: the same Worker powers `ach run`, a cron
// invocation, or a long-running daemon.
package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/pipeline"
	"github.com/27actions/ach/internal/ports"
)

// Worker runs pipeline jobs.
type Worker struct {
	deps pipeline.Deps
}

func New(d pipeline.Deps) *Worker {
	return &Worker{deps: d}
}

// Report summarizes one run pass.
type Report struct {
	Recovered int
	Ingested  int
	Skipped   int
	Completed int
	Failed    int
	Waiting   int
}

// Run performs one pass: requeue jobs abandoned by a crashed worker, re-drive
// orphaned submissions, ingest the intake, then dispatch every due job until
// the queue is empty.
func (w *Worker) Run(ctx context.Context, in ports.Input) (*Report, error) {
	if err := w.deps.Store.RequeueStaleJobs(ctx); err != nil {
		return nil, err
	}

	rep := &Report{}
	recovered, err := pipeline.ReconcileReceived(ctx, w.deps)
	if err != nil {
		return nil, err
	}
	rep.Recovered = recovered

	ir, err := pipeline.Ingest(ctx, w.deps, in)
	if err != nil {
		return nil, err
	}
	rep.Ingested = ir.Ingested
	rep.Skipped = ir.Skipped

	for {
		job, err := w.deps.Store.ClaimDueJob(ctx)
		if err != nil {
			return nil, err
		}
		if job == nil {
			break
		}
		switch w.runJob(ctx, job) {
		case outcomeDone:
			rep.Completed++
		case outcomeFailed:
			rep.Failed++
		case outcomeWaiting:
			rep.Waiting++
		}
	}
	return rep, nil
}

type outcome int

const (
	outcomeDone outcome = iota
	outcomeFailed
	outcomeWaiting
)

// runJob dispatches one claimed job and records the result. A job that cannot
// complete yet (ErrNotReady) is re-queued to run again shortly; a real error
// marks it failed; success completes it.
func (w *Worker) runJob(ctx context.Context, job *domain.Job) outcome {
	err := w.dispatch(ctx, job)
	switch {
	case errors.Is(err, pipeline.ErrNotReady):
		if rerr := w.deps.Store.EnqueueJob(ctx, job.Kind, job.Ref, w.deps.Clock.Now().Add(time.Minute)); rerr != nil {
			return outcomeFailed
		}
		return outcomeWaiting
	case err != nil:
		if ferr := w.deps.Store.FailJob(ctx, job.ID, err.Error()); ferr != nil {
			return outcomeFailed
		}
		return outcomeFailed
	default:
		if cerr := w.deps.Store.CompleteJob(ctx, job.ID); cerr != nil {
			return outcomeFailed
		}
		return outcomeDone
	}
}

func (w *Worker) dispatch(ctx context.Context, job *domain.Job) error {
	switch job.Kind {
	case domain.JobFixSubmission:
		return pipeline.FixSubmission(ctx, w.deps, job.Ref)
	case domain.JobImport:
		return pipeline.ImportSubmission(ctx, w.deps, job.Ref)
	case domain.JobScreen:
		_, err := pipeline.ScreenSubmission(ctx, w.deps, job.Ref)
		return err
	case domain.JobProcess:
		_, err := pipeline.ProcessSubmission(ctx, w.deps, job.Ref)
		return err
	case domain.JobPublishCleaned:
		return pipeline.PublishCleaned(ctx, w.deps, job.Ref)
	case domain.JobPublishRelease:
		return pipeline.PublishRelease(ctx, w.deps, job.Ref)
	case domain.JobArchive:
		return pipeline.ArchiveSubmission(ctx, w.deps, job.Ref)
	default:
		return fmt.Errorf("unknown job kind %q", job.Kind)
	}
}
