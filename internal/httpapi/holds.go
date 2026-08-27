package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/pipeline"

	"github.com/google/uuid"
)

type holdDetail struct {
	domain.Hold
	Reviews      []domain.Review `json:"reviews"`
	Velocity     *velocityInfo   `json:"velocity,omitempty"`
	ReleaseState string          `json:"release_state"`
	GroupSize    int             `json:"group_size"`
	GroupHolds   []domain.Hold   `json:"group_holds,omitempty"`
}

type velocityInfo struct {
	Total int64 `json:"total"`
	Prior int64 `json:"prior"`
	Held  int64 `json:"held"`
}

type reviewRequest struct {
	Note  string `json:"note"`
	Actor string `json:"actor"`
}

type bulkRequest struct {
	Action string      `json:"action"`
	IDs    []uuid.UUID `json:"ids"`
	Note   string      `json:"note"`
	Actor  string      `json:"actor"`
	Status string      `json:"status"`
}

type statusRequest struct {
	Status string `json:"status"`
	Note   string `json:"note"`
	Actor  string `json:"actor"`
	Scope  string `json:"scope"`
}

type holdsPage struct {
	Holds []domain.Hold `json:"holds"`
	Total int64         `json:"total"`
}

func (s *Server) handleListHolds(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := q.Get("status")
	search := q.Get("q")
	start := parseDate(q.Get("start_date"))
	end := parseDate(q.Get("end_date"))
	page, pageSize := pageParams(r)

	ctx := r.Context()
	total, err := s.deps.Store.CountHoldsFiltered(ctx, status, search, start, end)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows, err := s.deps.Store.ListHoldsFiltered(ctx, status, search, start, end, q.Get("sort"), q.Get("dir"), pageSize, (page-1)*pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, holdsPage{Holds: rows, Total: total})
}

func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.IDs) == 0 {
		writeErr(w, http.StatusBadRequest, errors.New("no holds selected"))
		return
	}
	if len(req.IDs) > 100 {
		writeErr(w, http.StatusBadRequest, errors.New("at most 100 holds may be selected"))
		return
	}
	if len(req.Note) > 2000 {
		writeErr(w, http.StatusBadRequest, errors.New("note is too long"))
		return
	}
	actor := s.reviewActor(r, req.Actor)
	var count int
	var err error
	switch req.Action {
	case "approve":
		count, err = pipeline.ApproveHolds(r.Context(), s.deps, req.IDs, actor, req.Note)
	case "decline":
		count, err = pipeline.DeclineHolds(r.Context(), s.deps, req.IDs, actor, req.Note)
	case "set_status":
		if req.Status == "" {
			writeErr(w, http.StatusBadRequest, errors.New("status is required"))
			return
		}
		count, err = pipeline.OverrideHolds(r.Context(), s.deps, req.IDs, actor, req.Note, domain.HoldStatus(req.Status))
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", req.Action))
		return
	}
	if errors.Is(err, pipeline.ErrInvalidHoldStatus) {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
}

func (s *Server) handleGetHold(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	h, err := s.deps.Store.GetHold(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	detail := holdDetail{Hold: h, ReleaseState: "none", GroupSize: 1}
	if reviews, err := s.deps.Store.ListReviewsByHold(ctx, id); err == nil {
		detail.Reviews = reviews
	} else {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if v := s.holdVelocity(ctx, h); v != nil {
		detail.Velocity = v
	}
	detail.ReleaseState = s.releaseState(ctx, h)
	if h.ReleaseArtifactID != nil {
		if members, err := s.deps.Store.ListHoldsByReleaseArtifact(ctx, *h.ReleaseArtifactID); err == nil && len(members) > 1 {
			detail.GroupSize = len(members)
			detail.GroupHolds = members
		}
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req, err := decodeReview(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	actor := s.reviewActor(r, req.Actor)
	if err := pipeline.ApproveHold(r.Context(), s.deps, id, actor, req.Note); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

func (s *Server) handleDecline(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	req, err := decodeReview(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	actor := s.reviewActor(r, req.Actor)
	if err := pipeline.DeclineHold(r.Context(), s.deps, id, actor, req.Note); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "declined"})
}

// handleSetStatus lets a reviewer change a hold's status directly (e.g.
// approved to declined) for record-keeping and future screening. It does not
// re-run the file pipeline; the change is audited via a review and only affects
// how later files to the same receiver pair are screened.
func (s *Server) handleSetStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	var req statusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if len(req.Note) > 2000 || len(req.Actor) > 200 {
		writeErr(w, http.StatusBadRequest, errors.New("review field is too long"))
		return
	}
	if req.Status == "" {
		writeErr(w, http.StatusBadRequest, errors.New("status is required"))
		return
	}
	actor := s.reviewActor(r, req.Actor)
	// Scope "hold" limits the change to this single hold; anything else (or
	// absent) applies it to the whole velocity group, matching how approve and
	// decline behave.
	group := req.Scope != "hold"
	if _, err := pipeline.OverrideHoldStatus(r.Context(), s.deps, id, actor, req.Note, domain.HoldStatus(req.Status), group); err != nil {
		if errors.Is(err, pipeline.ErrInvalidHoldStatus) {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": req.Status})
}

// holdVelocity reports the same-day group total for the hold's receiver
// account, summed across all ready submissions, so a reviewer can see the
// total exposure to that account.
func (s *Server) holdVelocity(ctx context.Context, h domain.Hold) *velocityInfo {
	if h.EffectiveDate == nil {
		return nil
	}
	sums, err := s.deps.Store.SumVelocity(ctx, time.Time{}, h.SubmissionID)
	if err != nil {
		return nil
	}
	var total int64
	found := false
	for i := range sums {
		v := &sums[i]
		if v.ReceiverAccount == h.EntryReceiverAcct && v.Rdfi == h.EntryRdfi &&
			v.EffectiveDate != nil && v.EffectiveDate.Equal(*h.EffectiveDate) &&
			v.CustomerID == h.CustomerID {
			total = v.Total
			found = true
			break
		}
	}
	if !found {
		return nil
	}

	// This submission's own contribution to the group; the rest already
	// shipped from earlier files before the velocity rule tripped.
	var held int64
	if entries, err := s.deps.Store.ListEntriesBySubmission(ctx, h.SubmissionID); err == nil {
		for _, e := range entries {
			if e.ReceiverAccount == h.EntryReceiverAcct && e.Rdfi == h.EntryRdfi &&
				e.EffectiveDate != nil && e.EffectiveDate.Equal(*h.EffectiveDate) &&
				e.CustomerID == h.CustomerID {
				held += e.Amount
			}
		}
	}
	prior := total - held
	if prior < 0 {
		prior = 0
	}
	return &velocityInfo{Total: total, Prior: prior, Held: held}
}

// releaseState describes the release artifact linked to a hold's submission.
func (s *Server) releaseState(ctx context.Context, h domain.Hold) string {
	if h.ReleaseArtifactID == nil {
		return "none"
	}
	release, err := s.deps.Store.GetArtifactByID(ctx, *h.ReleaseArtifactID)
	if err != nil {
		return "none"
	}
	if release.State == domain.ArtifactPublished {
		return "published"
	}
	holds, err := s.deps.Store.ListHoldsByReleaseArtifact(ctx, release.ID)
	if err != nil {
		return string(release.State)
	}
	for _, hh := range holds {
		if hh.Status == domain.HoldDeclined {
			return "blocked"
		}
		if hh.Status != domain.HoldApproved {
			return "awaiting_approval"
		}
	}
	return string(release.State)
}

func decodeReview(r *http.Request) (reviewRequest, error) {
	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, err
	}
	if len(req.Note) > 2000 || len(req.Actor) > 200 {
		return req, errors.New("review field is too long")
	}
	return req, nil
}
