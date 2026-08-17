package httpapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

type jobsPage struct {
	Jobs []domain.Job `json:"jobs"`
}

// handleListJobs lists the jobs in the given state (default failed). Super
// admins use this to find and requeue failed work from the web UI.
func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	state := domain.JobFailed
	if s := r.URL.Query().Get("state"); s != "" {
		state = domain.JobState(s)
	}
	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	if limit > 100 {
		limit = 100
	}

	jobs, err := s.deps.Store.ListJobsByState(r.Context(), state, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if jobs == nil {
		jobs = []domain.Job{}
	}
	writeJSON(w, http.StatusOK, jobsPage{Jobs: jobs})
}

// handleRequeueJob resets a failed job to queued so the next run retries it.
func (s *Server) handleRequeueJob(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.deps.Store.RequeueJob(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "requeued"})
}
