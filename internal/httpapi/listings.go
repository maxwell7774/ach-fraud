package httpapi

import (
	"errors"
	"net/http"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
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
