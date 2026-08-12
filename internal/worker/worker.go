// Package worker drives the pipeline by claiming and dispatching jobs from the
// outbox. It is transport-agnostic: the same Worker powers `ach run`, a cron
// invocation, or a long-running daemon.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"runtime/debug"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/pipeline"
	"github.com/27actions/ach/internal/ports"
)

// Worker runs pipeline jobs.
type Worker struct {
	deps pipeline.Deps
	// dispatchFn, when set, replaces the job dispatcher (tests use it to force
	// a panicking stage).
	dispatchFn func(ctx context.Context, job *domain.Job) error
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
// marks it failed; success completes it. A panic in a stage is recovered and
// recorded as a failed job, so one buggy stage can never crash the worker or
// block the rest of the queue.
func (w *Worker) runJob(ctx context.Context, job *domain.Job) (out outcome) {
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprintf("panic: %v\n%s", r, debug.Stack())
			if ferr := w.deps.Store.FailJob(ctx, job.ID, msg); ferr != nil {
				log.Printf("worker: recording panic for job %s: %v", job.ID, ferr)
			}
			w.emitFailure(ctx, job, msg)
			out = outcomeFailed
		}
	}()

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
		w.emitFailure(ctx, job, err.Error())
		return outcomeFailed
	default:
		if cerr := w.deps.Store.CompleteJob(ctx, job.ID); cerr != nil {
			return outcomeFailed
		}
		return outcomeDone
	}
}

// emitFailure records a job_failed event so the run digest can report it. Best
// effort: a failure to record must not mask the job failure itself.
func (w *Worker) emitFailure(ctx context.Context, job *domain.Job, reason string) {
	payload, err := json.Marshal(map[string]string{
		"kind":  string(job.Kind),
		"error": reason,
	})
	if err != nil {
		return
	}
	if eerr := pipeline.Emit(ctx, w.deps, pipeline.EvJobFailed, &job.Ref, payload); eerr != nil {
		log.Printf("worker: recording job_failed event for %s: %v", job.ID, eerr)
	}
}

func (w *Worker) dispatch(ctx context.Context, job *domain.Job) error {
	if w.dispatchFn != nil {
		return w.dispatchFn(ctx, job)
	}
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
