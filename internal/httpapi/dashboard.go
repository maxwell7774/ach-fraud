package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

type dashboard struct {
	HoldCounts        map[string]int64    `json:"hold_counts"`
	Pending           []domain.Hold       `json:"pending"`
	FailedSubmissions []domain.Submission `json:"failed_submissions"`
	FailedJobs        []failedJob         `json:"failed_jobs"`
	VelocityLeaks     []velocityLeak      `json:"velocity_leaks"`
}

type failedJob struct {
	ID           uuid.UUID      `json:"id"`
	SubmissionID *uuid.UUID     `json:"submission_id,omitempty"`
	Kind         domain.JobKind `json:"kind"`
	Filename     string         `json:"filename"`
	LastError    string         `json:"last_error"`
}

type velocityLeak struct {
	Account       string `json:"account"`
	Total         int64  `json:"total"`
	Prior         int64  `json:"prior"`
	Held          int64  `json:"held"`
	EffectiveDate string `json:"effective_date"`
	Filename      string `json:"filename,omitempty"`
}

// velocityAlert mirrors the payload written by pipeline/screen.go.
type velocityAlert struct {
	Account       string `json:"account"`
	Rdfi          string `json:"rdfi"`
	EffectiveDate string `json:"effective_date"`
	CustomerID    string `json:"customer_id"`
	Total         int64  `json:"total"`
	Prior         int64  `json:"prior"`
	Held          int64  `json:"held"`
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	counts, err := s.deps.Store.CountHoldsByStatus(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	pending, err := s.deps.Store.ListHoldsByStatus(ctx, string(domain.HoldPending), 50)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	failedSubs, err := s.deps.Store.ListSubmissions(ctx, string(domain.SubmissionFailed), 20)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if failedSubs == nil {
		failedSubs = []domain.Submission{}
	}

	failedJobs, err := s.deps.Store.ListJobsByState(ctx, domain.JobFailed, 20)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var jobs []failedJob
	for _, j := range failedJobs {
		fj := failedJob{ID: j.ID, Kind: j.Kind, LastError: j.LastError}
		// publish_release jobs reference a release artifact, not the submission.
		subID := j.Ref
		if _, err := s.deps.Store.GetSubmission(ctx, subID); err != nil {
			if a, aerr := s.deps.Store.GetArtifactByID(ctx, subID); aerr == nil {
				subID = a.SubmissionID
			}
		}
		if sub, err := s.deps.Store.GetSubmission(ctx, subID); err == nil {
			fj.Filename = sub.Filename
			id := sub.ID
			fj.SubmissionID = &id
		}
		jobs = append(jobs, fj)
	}

	var leaks []velocityLeak
	if events, err := s.deps.Store.ListEvents(ctx, 200, 0); err == nil {
		for _, ev := range events {
			if ev.Type != "velocity_crossed" {
				continue
			}
			var a velocityAlert
			if err := json.Unmarshal(ev.Payload, &a); err != nil || a.Prior <= 0 {
				continue
			}
			l := velocityLeak{
				Account: a.Account, Total: a.Total, Prior: a.Prior,
				Held: a.Held, EffectiveDate: a.EffectiveDate,
			}
			if ev.Ref != nil {
				if sub, err := s.deps.Store.GetSubmission(ctx, *ev.Ref); err == nil {
					l.Filename = sub.Filename
				}
			}
			leaks = append(leaks, l)
		}
	}
	if jobs == nil {
		jobs = []failedJob{}
	}
	if leaks == nil {
		leaks = []velocityLeak{}
	}

	writeJSON(w, http.StatusOK, dashboard{
		HoldCounts:        counts,
		Pending:           pending,
		FailedSubmissions: failedSubs,
		FailedJobs:        jobs,
		VelocityLeaks:     leaks,
	})
}
