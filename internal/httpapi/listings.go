package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/pipeline"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

type entriesPage struct {
	Entries []domain.BatchEntry `json:"entries"`
	Total   int64               `json:"total"`
}

type headersPage struct {
	Headers []domain.BatchHeader `json:"headers"`
	Total   int64                `json:"total"`
}

type eventsPage struct {
	Events []domain.Event `json:"events"`
	Total  int64          `json:"total"`
}

func (s *Server) handleListEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := q.Get("q")
	start := parseDate(q.Get("start_date"))
	end := parseDate(q.Get("end_date"))
	page, pageSize := pageParams(r)

	ctx := r.Context()
	total, err := s.deps.Store.CountEntriesFiltered(ctx, search, start, end)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows, err := s.deps.Store.ListEntriesFiltered(ctx, search, start, end, q.Get("sort"), q.Get("dir"), pageSize, (page-1)*pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, entriesPage{Entries: rows, Total: total})
}

func (s *Server) handleListHeaders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := q.Get("q")
	start := parseDate(q.Get("start_date"))
	end := parseDate(q.Get("end_date"))
	page, pageSize := pageParams(r)

	ctx := r.Context()
	total, err := s.deps.Store.CountHeadersFiltered(ctx, search, start, end)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows, err := s.deps.Store.ListHeadersFiltered(ctx, search, start, end, q.Get("sort"), q.Get("dir"), pageSize, (page-1)*pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, headersPage{Headers: rows, Total: total})
}

func (s *Server) handleArtifactContent(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	art, err := s.deps.Store.GetArtifactByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	data, err := s.deps.Files.Get(r.Context(), art.Checksum)
	if err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	search := q.Get("q")
	start := parseDate(q.Get("start_date"))
	end := parseDate(q.Get("end_date"))
	page, pageSize := pageParams(r)

	ctx := r.Context()
	total, err := s.deps.Store.CountEventsFiltered(ctx, search, start, end)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	events, err := s.deps.Store.ListEventsFiltered(ctx, search, start, end, q.Get("sort"), q.Get("dir"), pageSize, (page-1)*pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, eventsPage{Events: events, Total: total})
}

func (s *Server) handleGetEntry(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	e, err := s.deps.Store.GetEntryByID(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *Server) handleCreateHold(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
		Actor  string `json:"actor"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Reason) > 2000 {
		writeErr(w, http.StatusBadRequest, errors.New("reason is too long"))
		return
	}
	status := domain.HoldStatus(req.Status)
	if status == "" {
		status = domain.HoldPending
	}
	actor := s.reviewActor(r, req.Actor)
	holdID, err := pipeline.ManuallyCreateHold(r.Context(), s.deps, id, actor, req.Reason, status)
	if err != nil {
		if errors.Is(err, pipeline.ErrInvalidHoldStatus) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if isDuplicateHoldError(err) {
			writeErr(w, http.StatusConflict, errors.New("a hold already exists for this entry"))
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, errors.New("entry not found"))
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": holdID.String(), "status": string(status)})
}

// isDuplicateHoldError reports whether err represents a unique-violation on the
// holds table. It detects both the fake-store error string and the real Postgres
// error code (23505) so the check works regardless of locale.
func isDuplicateHoldError(err error) bool {
	if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "duplicate") {
		return true
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
