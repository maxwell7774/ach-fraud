package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
	"github.com/27actions/ach/internal/notifier"
	"github.com/27actions/ach/internal/pipeline"

	"github.com/google/uuid"
)

func testServer(t *testing.T) (*Server, *fakeports.Store) {
	t.Helper()
	st := fakeports.NewStore()
	d := pipeline.Deps{
		Store:    st,
		Files:    fakeports.NewFiles(),
		Sender:   fakeports.NewSender(),
		Notifier: notifier.Noop{},
		Clock:    fakeports.NewClock(time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)),
		Policy: domain.Policy{
			HoldDays: 30, HoldingRDFI: "333333334", HoldingAccount: "555555",
			HoldSingleAmount: 100000, HoldVelocityAmount: 100000,
		},
	}
	s := New(d)
	s.staticDir = "/nonexistent"
	return s, st
}

func seedHold(t *testing.T, st *fakeports.Store) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "a.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1"})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
		HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "100000001",
		Amount: 200000, TranCode: 22, Trace: "t1",
	})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	h, err := st.CreateHold(ctx, en.ID, domain.HoldPending, "")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	return h.ID
}

func get(t *testing.T, s *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	return rr
}

func TestHealthz(t *testing.T) {
	s, _ := testServer(t)
	rr := get(t, s, "/healthz")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
}

func TestDashboard(t *testing.T) {
	s, st := testServer(t)
	seedHold(t, st)
	rr := get(t, s, "/api/dashboard")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var d dashboard
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.HoldCounts["pending"] != 1 || len(d.Pending) != 1 {
		t.Fatalf("unexpected dashboard: %+v", d)
	}
}

func TestListHoldsFilter(t *testing.T) {
	s, st := testServer(t)
	seedHold(t, st)
	rr := get(t, s, "/api/holds?status=approved")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var page holdsPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Holds) != 0 || page.Total != 0 {
		t.Fatalf("expected no approved holds, got %+v", page)
	}

	rr = get(t, s, "/api/holds?status=pending")
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Holds) != 1 || page.Total != 1 {
		t.Fatalf("expected 1 pending hold, got %+v", page)
	}
}

func TestGetHoldNotFound(t *testing.T) {
	s, _ := testServer(t)
	rr := get(t, s, "/api/holds/"+uuid.New().String())
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestApproveHold(t *testing.T) {
	s, st := testServer(t)
	id := seedHold(t, st)

	body := `{"note":"looks fine","actor":"reviewer-a"}`
	req := httptest.NewRequest(http.MethodPost, "/api/holds/"+id.String()+"/approve", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	h, err := st.GetHold(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if h.Status != domain.HoldApproved {
		t.Fatalf("status = %s, want approved", h.Status)
	}
	reviews, _ := st.ListReviewsByHold(context.Background(), id)
	if len(reviews) != 1 || reviews[0].Actor != "reviewer-a" || reviews[0].Note != "looks fine" {
		t.Fatalf("unexpected reviews: %+v", reviews)
	}
}

func TestDeclineHoldConflictOnReviewed(t *testing.T) {
	s, st := testServer(t)
	id := seedHold(t, st)
	ctx := context.Background()
	if err := pipeline.ApproveHold(ctx, s.deps, id, "a", ""); err != nil {
		t.Fatalf("approve: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/holds/"+id.String()+"/decline", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (already approved)", rr.Code)
	}
}

func TestActorFromRequestErrors(t *testing.T) {
	s, st := testServer(t)
	id := seedHold(t, st)
	req := httptest.NewRequest(http.MethodPost, "/api/holds/"+id.String()+"/approve", strings.NewReader("not-json"))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}
