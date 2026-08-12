package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/27actions/ach/internal/domain"

	"github.com/google/uuid"
)

// recipientRequest is the create/update body for an email alert recipient.
type recipientRequest struct {
	Email      string   `json:"email"`
	Name       string   `json:"name"`
	Enabled    *bool    `json:"enabled"`
	AlertTypes []string `json:"alert_types"`
}

var knownAlertTypes = map[string]bool{
	domain.AlertPendingHolds:   true,
	domain.AlertVelocityLeaks:  true,
	domain.AlertReleaseBlocked: true,
	domain.AlertFailed:         true,
}

func (s *Server) handleListRecipients(w http.ResponseWriter, r *http.Request) {
	recs, err := s.deps.Store.ListRecipients(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if recs == nil {
		recs = []domain.Recipient{}
	}
	writeJSON(w, http.StatusOK, map[string][]domain.Recipient{"recipients": recs})
}

func (s *Server) handleCreateRecipient(w http.ResponseWriter, r *http.Request) {
	req, err := decodeRecipient(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if req.Email == "" {
		writeErr(w, http.StatusBadRequest, errors.New("email is required"))
		return
	}
	rec, err := s.deps.Store.CreateRecipient(r.Context(), domain.Recipient{
		Email:      req.Email,
		Name:       req.Name,
		Enabled:    enabledOr(req.Enabled, true),
		AlertTypes: req.AlertTypes,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, rec)
}

func (s *Server) handleUpdateRecipient(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req, err := decodeRecipient(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	rec, err := s.deps.Store.UpdateRecipient(r.Context(), domain.Recipient{
		ID:         id,
		Name:       req.Name,
		Enabled:    enabledOr(req.Enabled, true),
		AlertTypes: req.AlertTypes,
	})
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

func (s *Server) handleDeleteRecipient(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.deps.Store.DeleteRecipient(r.Context(), id); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func decodeRecipient(r *http.Request) (recipientRequest, error) {
	var req recipientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, err
	}
	// Reject alert types outside the known set so a typo cannot silently
	// subscribe someone to nothing.
	for _, t := range req.AlertTypes {
		if !knownAlertTypes[t] {
			return req, errors.New("unknown alert type " + t)
		}
	}
	return req, nil
}

func enabledOr(v *bool, def bool) bool {
	if v == nil {
		return def
	}
	return *v
}
