package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/auth"
	"github.com/27actions/ach/internal/checksum"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/fakeports"
	"github.com/27actions/ach/internal/notifier"
	"github.com/27actions/ach/internal/pipeline"
	"github.com/27actions/ach/internal/testutil"

	"github.com/google/uuid"
	"github.com/moov-io/ach"
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

func TestDashboardCountsWindowed(t *testing.T) {
	s, st := testServer(t)
	fixed := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	st.Now = func() time.Time { return fixed }
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "w.ach", Status: domain.SubmissionReady, ReceivedAt: fixed})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1"})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	mkEntry := func(trace string) uuid.UUID {
		en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
			HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "100000001",
			Amount: 200000, TranCode: 22, Trace: trace,
		})
		if err != nil {
			t.Fatalf("entry: %v", err)
		}
		return en.ID
	}
	hold := func(entry uuid.UUID, status domain.HoldStatus) uuid.UUID {
		h, err := st.CreateHold(ctx, entry, status, "")
		if err != nil {
			t.Fatalf("hold: %v", err)
		}
		return h.ID
	}

	// Pending (always counted) and decisions made at `fixed` (inside the
	// 7-day window).
	hold(mkEntry("p1"), domain.HoldPending)
	recentApproved := hold(mkEntry("a1"), domain.HoldPending)
	if err := st.SetHoldStatus(ctx, recentApproved, domain.HoldApproved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	recentDeclined := hold(mkEntry("d1"), domain.HoldPending)
	if err := st.SetHoldStatus(ctx, recentDeclined, domain.HoldDeclined); err != nil {
		t.Fatalf("decline: %v", err)
	}

	// Decisions made 8 days before `fixed`, outside the 7-day window.
	st.Now = func() time.Time { return fixed.Add(-8 * 24 * time.Hour) }
	oldApproved := hold(mkEntry("a2"), domain.HoldPending)
	if err := st.SetHoldStatus(ctx, oldApproved, domain.HoldApproved); err != nil {
		t.Fatalf("approve old: %v", err)
	}
	hold(mkEntry("e1"), domain.HoldAutoDeclined)

	rr := get(t, s, "/api/dashboard")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var d dashboard
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.HoldCounts["pending"] != 1 {
		t.Fatalf("pending = %d, want 1", d.HoldCounts["pending"])
	}
	if d.HoldCounts["approved"] != 1 {
		t.Fatalf("approved = %d, want 1", d.HoldCounts["approved"])
	}
	if d.HoldCounts["declined"] != 1 {
		t.Fatalf("declined = %d, want 1", d.HoldCounts["declined"])
	}
	if d.HoldCounts["auto_declined"] != 0 {
		t.Fatalf("auto_declined = %d, want 0", d.HoldCounts["auto_declined"])
	}
}

func TestListHoldsSortByCustomer(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "s.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	mk := func(customer, account, trace string) {
		hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: customer})
		if err != nil {
			t.Fatalf("hdr: %v", err)
		}
		en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
			HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: account,
			Amount: 200000, TranCode: 22, Trace: trace,
		})
		if err != nil {
			t.Fatalf("entry: %v", err)
		}
		if _, err := st.CreateHold(ctx, en.ID, domain.HoldPending, ""); err != nil {
			t.Fatalf("hold: %v", err)
		}
	}
	mk("zeta", "100000002", "t2")
	mk("alpha", "100000001", "t1")

	var page holdsPage
	rr := get(t, s, "/api/holds?sort=customer&dir=asc")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Holds) != 2 {
		t.Fatalf("holds = %d, want 2", len(page.Holds))
	}
	if page.Holds[0].CustomerID != "alpha" || page.Holds[1].CustomerID != "zeta" {
		t.Fatalf("customer order = %q, %q; want alpha, zeta", page.Holds[0].CustomerID, page.Holds[1].CustomerID)
	}
}

func TestJobsListAndRequeue(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()

	ref := uuid.New()
	if err := st.EnqueueJob(ctx, domain.JobFixSubmission, ref, time.Now()); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := st.ClaimDueJob(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job == nil {
		t.Fatal("no job claimed")
	}
	if err := st.FailJob(ctx, job.ID, "boom"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	var page jobsPage
	rr := get(t, s, "/api/jobs?state=failed")
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Jobs) != 1 || page.Jobs[0].State != domain.JobFailed || page.Jobs[0].LastError != "boom" {
		t.Fatalf("jobs = %+v", page.Jobs)
	}

	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/api/jobs/"+job.ID.String()+"/requeue", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("requeue = %d %s", rr.Code, rr.Body.String())
	}

	rr = get(t, s, "/api/jobs?state=queued")
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Jobs) != 1 || page.Jobs[0].State != domain.JobQueued || page.Jobs[0].LastError != "" {
		t.Fatalf("jobs after requeue = %+v", page.Jobs)
	}

	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/api/jobs/"+uuid.NewString()+"/requeue", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("requeue unknown = %d, want 404", rr.Code)
	}
}

func TestRecipientsCRUD(t *testing.T) {
	s, _ := testServer(t)

	var rec domain.Recipient
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/api/recipients",
		strings.NewReader(`{"email":"ops@corp.com","name":"Ops","enabled":true,"alert_types":["pending_holds","failed"]}`)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &rec); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rec.Email != "ops@corp.com" || len(rec.AlertTypes) != 2 {
		t.Fatalf("created = %+v", rec)
	}

	var page struct {
		Recipients []domain.Recipient `json:"recipients"`
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/recipients", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Recipients) != 1 {
		t.Fatalf("recipients = %d, want 1", len(page.Recipients))
	}

	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("PUT", "/api/recipients/"+rec.ID.String(),
		strings.NewReader(`{"email":"alerts@corp.com","name":"Ops2","enabled":false,"alert_types":["velocity_leaks"]}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("update = %d %s", rr.Code, rr.Body.String())
	}
	var up domain.Recipient
	if err := json.Unmarshal(rr.Body.Bytes(), &up); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if up.Email != "alerts@corp.com" || up.Name != "Ops2" || up.Enabled || len(up.AlertTypes) != 1 || up.AlertTypes[0] != "velocity_leaks" {
		t.Fatalf("updated = %+v", up)
	}

	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("DELETE", "/api/recipients/"+rec.ID.String(), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("delete = %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/recipients", nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Recipients) != 0 {
		t.Fatalf("recipients after delete = %d, want 0", len(page.Recipients))
	}
}

func TestRecipientsRejectsUnknownAlertType(t *testing.T) {
	s, _ := testServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/api/recipients",
		strings.NewReader(`{"email":"ops@corp.com","alert_types":["bogus"]}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("create = %d, want 400 (%s)", rr.Code, rr.Body.String())
	}
}

func TestRecipientWithoutAlertsReturnsEmptyArray(t *testing.T) {
	s, _ := testServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("POST", "/api/recipients",
		strings.NewReader(`{"email":"quiet@corp.com","alert_types":[]}`)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/recipients", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d %s", rr.Code, rr.Body.String())
	}
	var page struct {
		Recipients []domain.Recipient `json:"recipients"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(page.Recipients) != 1 || page.Recipients[0].AlertTypes == nil {
		t.Fatalf("recipients = %+v; expected non-nil empty alert_types", page.Recipients)
	}
}

func TestVelocityLeakScopedToLiveGroup(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	eff := time.Date(2026, 1, 14, 0, 0, 0, 0, time.UTC)
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "leak.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1", EffectiveDate: &eff})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
		HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "100000001",
		Amount: 60000, TranCode: 22, Trace: "t1",
	})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	hold, err := st.CreateHold(ctx, en.ID, domain.HoldPending, "")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	payload := []byte(`{"account":"100000001","rdfi":"231380104","effective_date":"2026-01-14","customer_id":"c1","total":120000,"prior":60000,"held":60000}`)
	if err := st.AppendEvent(ctx, "velocity_crossed", &sub.ID, payload); err != nil {
		t.Fatalf("event: %v", err)
	}

	var d dashboard
	rr := get(t, s, "/api/dashboard")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(d.VelocityLeaks) != 1 || d.VelocityLeaks[0].Prior != 60000 || d.VelocityLeaks[0].Filename != "leak.ach" {
		t.Fatalf("leaks = %+v, want one live leak", d.VelocityLeaks)
	}

	// Decide the group's hold: the leak is no longer live and drops off.
	if err := st.SetHoldStatus(ctx, hold.ID, domain.HoldApproved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	rr = get(t, s, "/api/dashboard")
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(d.VelocityLeaks) != 0 {
		t.Fatalf("leaks still shown after group decided: %+v", d.VelocityLeaks)
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

func TestSetHoldStatus(t *testing.T) {
	s, st := testServer(t)
	id := seedHold(t, st)

	body := `{"status":"declined","note":"override","actor":"reviewer-a","scope":"hold"}`
	req := httptest.NewRequest(http.MethodPost, "/api/holds/"+id.String()+"/status", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	h, err := st.GetHold(context.Background(), id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if h.Status != domain.HoldDeclined {
		t.Fatalf("status = %s, want declined", h.Status)
	}
	reviews, _ := st.ListReviewsByHold(context.Background(), id)
	if len(reviews) != 1 || reviews[0].Action != "declined" || reviews[0].Actor != "reviewer-a" {
		t.Fatalf("unexpected reviews: %+v", reviews)
	}
}

func TestSetHoldStatusRejectsInvalid(t *testing.T) {
	s, st := testServer(t)
	id := seedHold(t, st)

	req := httptest.NewRequest(http.MethodPost, "/api/holds/"+id.String()+"/status", strings.NewReader(`{"status":"bogus"}`))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for invalid status", rr.Code)
	}
	h, _ := st.GetHold(context.Background(), id)
	if h.Status != domain.HoldPending {
		t.Fatalf("status = %s, want pending unchanged", h.Status)
	}
}

func TestGetEntry(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, _ := st.CreateSubmission(ctx, domain.Submission{Filename: "test.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1"})
	en, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "111111", Amount: 50000, TranCode: 22, Trace: "t1"})

	req := httptest.NewRequest(http.MethodGet, "/api/entries/"+en.ID.String(), nil)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}
	var got domain.BatchEntry
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Trace != "t1" || got.ReceiverAccount != "111111" || got.Amount != 50000 {
		t.Fatalf("unexpected entry: %+v", got)
	}
	if got.SubmissionID == "" {
		t.Fatal("expected submission_id to be populated")
	}
}

func TestCreateHold(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, _ := st.CreateSubmission(ctx, domain.Submission{Filename: "test.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1"})
	en, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "111111", Amount: 50000, TranCode: 22, Trace: "t1"})

	body := `{"status":"declined","reason":"suspicious","actor":"analyst"}`
	req := httptest.NewRequest(http.MethodPost, "/api/entries/"+en.ID.String()+"/hold", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rr.Code, rr.Body.String())
	}

	holds, _ := st.ListAllHolds(ctx)
	if len(holds) != 1 {
		t.Fatalf("expected 1 hold, got %d", len(holds))
	}
	if holds[0].Status != domain.HoldDeclined || holds[0].EntryID != en.ID {
		t.Fatalf("unexpected hold: %+v", holds[0])
	}
	reviews, _ := st.ListReviewsByHold(ctx, holds[0].ID)
	if len(reviews) != 1 || reviews[0].Actor != "analyst" {
		t.Fatalf("unexpected reviews: %+v", reviews)
	}
}

func TestCreateHoldDuplicate(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, _ := st.CreateSubmission(ctx, domain.Submission{Filename: "test.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	hdr, _ := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1"})
	en, _ := st.CreateBatchEntry(ctx, domain.BatchEntry{HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "111111", Amount: 50000, TranCode: 22, Trace: "t1"})
	st.CreateHold(ctx, en.ID, domain.HoldPending, "existing")

	body := `{"status":"declined","reason":"dup","actor":"analyst"}`
	req := httptest.NewRequest(http.MethodPost, "/api/entries/"+en.ID.String()+"/hold", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rr.Code)
	}
}

func TestCreateHoldNotFound(t *testing.T) {
	s, _ := testServer(t)
	fakeID := uuid.New()
	body := `{"status":"approved","reason":"test","actor":"analyst"}`
	req := httptest.NewRequest(http.MethodPost, "/api/entries/"+fakeID.String()+"/hold", strings.NewReader(body))
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
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

// --- auth ---

type fakeEntra struct {
	loginURL  string
	ident     auth.Identity
	err       error
	exchanged string
}

func (f *fakeEntra) LoginURL(state, verifier string) string {
	return f.loginURL + "?state=" + state
}

func (f *fakeEntra) Exchange(_ context.Context, code, verifier string) (auth.Identity, error) {
	f.exchanged = verifier
	return f.ident, f.err
}

func testAuthServer(t *testing.T) (*Server, *fakeports.Store, *fakeEntra) {
	t.Helper()
	s, st := testServer(t)
	fe := &fakeEntra{
		loginURL: "https://login.example/authorize",
		ident: auth.Identity{
			Subject: "sub-1", UPN: "alice@corp.com", Name: "Alice",
			Roles: []string{"ACH.Processor"},
		},
	}
	s.EnableAuth(fe, &auth.SessionManager{Store: st, TTL: time.Hour}, false)
	return s, st, fe
}

func cookieOf(t *testing.T, rr *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, c := range rr.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	t.Fatalf("cookie %s not set", name)
	return ""
}

func TestAuthDisabledByDefault(t *testing.T) {
	s, _ := testServer(t)
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/dashboard", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("auth-disabled dashboard = %d, want 200", rr.Code)
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/me", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("/api/auth/me when disabled = %d, want 404", rr.Code)
	}
}

func TestAuthFullFlow(t *testing.T) {
	s, st, fe := testAuthServer(t)
	h := s.Handler()

	// Without a session, the API is locked down.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/dashboard", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard without session = %d, want 401", rr.Code)
	}

	// Login redirects to Entra with the oauth state cookie.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/login", nil))
	if rr.Code != http.StatusFound {
		t.Fatalf("login = %d, want 302", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc == "" || !strings.HasPrefix(loc, fe.loginURL) {
		t.Fatalf("login location = %q", loc)
	}
	oauth := cookieOf(t, rr, auth.OAuthCookieName)
	parts := strings.SplitN(oauth, "|", 2)
	if len(parts) != 2 {
		t.Fatalf("oauth cookie = %q", oauth)
	}

	// Callback exchanges the code, upserts the user, and issues a session.
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/callback?code=abc&state="+parts[0], nil).WithContext(context.Background()))
	// re-serve with the cookie set
	req := httptest.NewRequest("GET", "/api/auth/callback?code=abc&state="+parts[0], nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthCookieName, Value: oauth})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("callback = %d, want 302 (body %s)", rr.Code, rr.Body.String())
	}
	if fe.exchanged != parts[1] {
		t.Fatalf("verifier passed to exchange = %q, want %q", fe.exchanged, parts[1])
	}
	sess := cookieOf(t, rr, auth.SessionCookieName)
	users := st.Users()
	if len(users) != 1 || users[0].Subject != "sub-1" || users[0].Name != "Alice" {
		t.Fatalf("users = %+v", users)
	}

	// The session cookie now unlocks the API, and /me reports the user.
	req = httptest.NewRequest("GET", "/api/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sess})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("dashboard with session = %d, want 200", rr.Code)
	}

	req = httptest.NewRequest("GET", "/api/auth/me", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sess})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"name":"Alice"`) {
		t.Fatalf("me = %d %s", rr.Code, rr.Body.String())
	}

	// Logout invalidates the session; the API locks again.
	req = httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sess})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("logout = %d", rr.Code)
	}
	req = httptest.NewRequest("GET", "/api/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sess})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard after logout = %d, want 401", rr.Code)
	}
}

func TestAuthStateMismatch(t *testing.T) {
	s, _, _ := testAuthServer(t)
	h := s.Handler()
	req := httptest.NewRequest("GET", "/api/auth/callback?code=abc&state=wrong", nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthCookieName, Value: "right|verifier"})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("state mismatch = %d, want 400", rr.Code)
	}
}

func TestAuthActorFromSession(t *testing.T) {
	s, st, _ := testAuthServer(t)
	h := s.Handler()

	// Log in as Alice.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/login", nil))
	oauth := cookieOf(t, rr, auth.OAuthCookieName)
	parts := strings.SplitN(oauth, "|", 2)
	req := httptest.NewRequest("GET", "/api/auth/callback?code=abc&state="+parts[0], nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthCookieName, Value: oauth})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	sess := cookieOf(t, rr, auth.SessionCookieName)

	// Approve a hold; the review actor must be the signed-in user, even when
	// the client tries to spoof a different one in the body.
	holdID := seedHold(t, st)
	body := strings.NewReader(`{"actor":"Imposter"}`)
	req = httptest.NewRequest("POST", "/api/holds/"+holdID.String()+"/approve", body)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sess})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("approve = %d %s", rr.Code, rr.Body.String())
	}
	reviews := st.Reviews()
	if len(reviews) != 1 || reviews[0].Actor != "Alice" {
		t.Fatalf("reviews = %+v, want actor Alice", reviews)
	}
}

// loginAs completes the OAuth flow for a user holding the given Entra roles
// and returns the session cookie. Each call uses a distinct subject so role
// combinations map to distinct users.
func loginAs(t *testing.T, s *Server, fe *fakeEntra, roles []string) string {
	t.Helper()
	fe.ident.Subject = "sub-" + strings.Join(roles, "-")
	fe.ident.Roles = roles
	h := s.Handler()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/auth/login", nil))
	oauth := cookieOf(t, rr, auth.OAuthCookieName)
	parts := strings.SplitN(oauth, "|", 2)
	req := httptest.NewRequest("GET", "/api/auth/callback?code=abc&state="+parts[0], nil)
	req.AddCookie(&http.Cookie{Name: auth.OAuthCookieName, Value: oauth})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return cookieOf(t, rr, auth.SessionCookieName)
}

func withCookie(r *http.Request, sess string) *http.Request {
	r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: sess})
	return r
}

func TestAuthorizationMatrix(t *testing.T) {
	s, st, fe := testAuthServer(t)
	holdID := seedHold(t, st)

	watcher := loginAs(t, s, fe, []string{"ACH.Watcher"})
	processor := loginAs(t, s, fe, []string{"ACH.Processor"})
	admin := loginAs(t, s, fe, []string{"ACH.Admin"})

	// Everyone can view the dashboard, holds, files, and entries.
	for _, sess := range []string{watcher, processor, admin} {
		for _, p := range []string{"/api/dashboard", "/api/holds", "/api/submissions", "/api/entries"} {
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", p, nil), sess))
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", p, rr.Code)
			}
		}
	}

	// Watchers take no action: no approve/decline, no manual flagging, no raw
	// bytes, and no events/jobs access.
	for _, p := range []string{"/api/events", "/api/jobs"} {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", p, nil), watcher))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("watcher %s = %d, want 403", p, rr.Code)
		}
	}
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/holds/"+holdID.String()+"/approve", strings.NewReader(`{}`)), watcher))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("watcher approve = %d, want 403", rr.Code)
	}

	// Processors review holds but cannot manually flag entries or view raw bytes.
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/holds/"+holdID.String()+"/approve", strings.NewReader(`{}`)), processor))
	if rr.Code != http.StatusOK {
		t.Fatalf("processor approve = %d %s, want 200", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", "/api/artifacts/"+holdID.String()+"/content", nil), processor))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("processor artifact content = %d, want 403", rr.Code)
	}

	// Admins review holds and manually flag entries, but cannot view raw bytes
	// and cannot see events or manage email recipients (super-admin only).
	adminHold := seedHold(t, st)
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/holds/"+adminHold.String()+"/decline", strings.NewReader(`{}`)), admin))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin decline = %d, want 200", rr.Code)
	}
	for _, p := range []string{"/api/submissions", "/api/entries", "/api/headers"} {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", p, nil), admin))
		if rr.Code != http.StatusOK {
			t.Fatalf("admin %s = %d, want 200", p, rr.Code)
		}
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", "/api/artifacts/"+adminHold.String()+"/content", nil), admin))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("admin artifact content = %d, want 403", rr.Code)
	}
	for _, p := range []string{"/api/events", "/api/recipients", "/api/jobs"} {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", p, nil), admin))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("admin %s = %d, want 403", p, rr.Code)
		}
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/recipients",
		strings.NewReader(`{"email":"a@b.c","alert_types":["pending_holds"]}`)), admin))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("admin create recipient = %d, want 403", rr.Code)
	}

	// Super admins do everything, including events and recipients.
	superAdmin := loginAs(t, s, fe, []string{"ACH.SuperAdmin"})
	for _, p := range []string{"/api/dashboard", "/api/holds", "/api/submissions", "/api/entries", "/api/headers", "/api/events", "/api/recipients", "/api/jobs"} {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", p, nil), superAdmin))
		if rr.Code != http.StatusOK {
			t.Fatalf("super admin %s = %d, want 200", p, rr.Code)
		}
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/recipients",
		strings.NewReader(`{"email":"a@b.c","alert_types":["pending_holds"]}`)), superAdmin))
	if rr.Code != http.StatusCreated {
		t.Fatalf("super admin create recipient = %d, want 201", rr.Code)
	}
}

func TestEntryHoldIsAdminOnly(t *testing.T) {
	s, st, fe := testAuthServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "flag.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1"})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
		HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "100000001",
		Amount: 5000, TranCode: 22, Trace: "t1",
	})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	flagBody := `{"status":"approved","reason":"manual"}`
	for _, tc := range []struct {
		name  string
		roles []string
		want  int
	}{
		{"watcher", []string{"ACH.Watcher"}, http.StatusForbidden},
		{"processor", []string{"ACH.Processor"}, http.StatusForbidden},
		{"admin", []string{"ACH.Admin"}, http.StatusOK},
	} {
		cookie := loginAs(t, s, fe, tc.roles)
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/entries/"+en.ID.String()+"/hold", strings.NewReader(flagBody)), cookie))
		if rr.Code != tc.want {
			t.Fatalf("%s flag entry = %d, want %d", tc.name, rr.Code, tc.want)
		}
	}
}

func TestAuthMeReportsRole(t *testing.T) {
	s, _, fe := testAuthServer(t)
	sess := loginAs(t, s, fe, []string{"ACH.Processor"})
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", "/api/auth/me", nil), sess))
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"role":"processor"`) {
		t.Fatalf("me = %d %s", rr.Code, rr.Body.String())
	}
}

func TestVerifyReportsPruned(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "p.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	art, err := st.CreateArtifact(ctx, domain.Artifact{
		SubmissionID: sub.ID, Kind: domain.ArtifactOriginal, Checksum: "abc123", State: domain.ArtifactPublished,
	})
	if err != nil {
		t.Fatalf("artifact: %v", err)
	}
	if err := s.deps.Files.Put(ctx, "abc123", []byte("ach bytes")); err != nil {
		t.Fatalf("put: %v", err)
	}
	// Retire the artifact and remove its bytes, as prune does.
	if err := st.SetArtifactState(ctx, art.ID, domain.ArtifactPruned); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if err := s.deps.Files.Delete(ctx, "abc123"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/submissions/"+sub.ID.String()+"/verify", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", rr.Code, rr.Body.String())
	}
	var v submissionVerify
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v.Pruned {
		t.Fatalf("verify.pruned = false, want true: %s", rr.Body.String())
	}
	sum := v.Artifacts["original"]
	if !sum.Pruned || sum.Present || sum.Error != "" {
		t.Fatalf("original sum = %+v, want pruned (present=false, no error)", sum)
	}
	if len(v.Issues) != 1 || !strings.Contains(v.Issues[0], "pruned; verification unavailable") {
		t.Fatalf("issues = %v, want a pruned note", v.Issues)
	}
}

func TestSubmissionDetailPaginatesHolds(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "many.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	hdr, err := st.CreateBatchHeader(ctx, domain.BatchHeader{SubmissionID: sub.ID, CustomerID: "c1"})
	if err != nil {
		t.Fatalf("hdr: %v", err)
	}
	for i := 0; i < 60; i++ {
		en, err := st.CreateBatchEntry(ctx, domain.BatchEntry{
			HeaderID: hdr.ID, Rdfi: "231380104", ReceiverAccount: "100000001",
			Amount: 200000, TranCode: 22, Trace: fmt.Sprintf("t%d", i),
		})
		if err != nil {
			t.Fatalf("entry: %v", err)
		}
		if _, err := st.CreateHold(ctx, en.ID, domain.HoldPending, ""); err != nil {
			t.Fatalf("hold: %v", err)
		}
	}

	var d submissionDetail
	rr := get(t, s, "/api/submissions/"+sub.ID.String()+"?page=2&pageSize=25")
	if rr.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.HoldsTotal != 60 {
		t.Fatalf("holds_total = %d, want 60", d.HoldsTotal)
	}
	if len(d.Holds) != 25 {
		t.Fatalf("page 2 holds = %d, want 25", len(d.Holds))
	}
	rr = get(t, s, "/api/submissions/"+sub.ID.String()+"?page=3&pageSize=25")
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(d.Holds) != 10 {
		t.Fatalf("page 3 holds = %d, want 10", len(d.Holds))
	}
}

func TestVerifyPaginatesEntries(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "entries.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	ents := make([]testutil.Entry, 60)
	for i := range ents {
		ents[i] = testutil.Entry{Account: "100000001", Name: "Jane Doe", Amount: 1000, RDFI: "231380104", Trace: i + 1}
	}
	data, err := testutil.CreditFile(ents, "260115")
	if err != nil {
		t.Fatalf("credit file: %v", err)
	}
	sum := checksum.Bytes(data)
	for _, kind := range []domain.ArtifactKind{domain.ArtifactOriginal, domain.ArtifactFixed} {
		if _, err := st.CreateArtifact(ctx, domain.Artifact{
			SubmissionID: sub.ID, Kind: kind, Checksum: sum, State: domain.ArtifactPublished,
		}); err != nil {
			t.Fatalf("artifact %s: %v", kind, err)
		}
	}
	if err := s.deps.Files.Put(ctx, sum, data); err != nil {
		t.Fatalf("put: %v", err)
	}

	var v submissionVerify
	rr := get(t, s, "/api/submissions/"+sub.ID.String()+"/verify?page=2&pageSize=25")
	if rr.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.EntriesTotal != 60 {
		t.Fatalf("entries_total = %d, want 60", v.EntriesTotal)
	}
	if len(v.Entries) != 25 {
		t.Fatalf("page 2 entries = %d, want 25", len(v.Entries))
	}
}

func TestSubmissionDetailIncludesVerification(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "v.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	if err := st.UpsertVerification(ctx, sub.ID, true, ""); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var d submissionDetail
	rr := get(t, s, "/api/submissions/"+sub.ID.String())
	if rr.Code != http.StatusOK {
		t.Fatalf("detail = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if d.Verification == nil || !d.Verification.Verified {
		t.Fatalf("verification = %+v, want verified", d.Verification)
	}
}

func TestVerifyIncludesSenderAndRelease(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	sub, err := st.CreateSubmission(ctx, domain.Submission{Filename: "v.ach", Status: domain.SubmissionReady, ReceivedAt: time.Now()})
	if err != nil {
		t.Fatalf("sub: %v", err)
	}
	data, err := testutil.CreditFile([]testutil.Entry{
		{Account: "111111", Name: "Jane", Identification: "123456789", Amount: 200000, RDFI: "231380104", Trace: 1},
		{Account: "222222", Name: "B", Amount: 5000, RDFI: "231380104", Trace: 2},
	}, "260115")
	if err != nil {
		t.Fatalf("credit file: %v", err)
	}
	fixed, err := achp.Read(data, true)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	trace0 := fixed.Batches[0].GetEntries()[0].TraceNumberField()
	held, err := achp.MatchHolds(fixed, []domain.Hold{
		{ID: uuid.New(), EntryTrace: trace0, EntryRdfi: "231380104", EntryReceiverAcct: "111111", EntryAmount: 200000, Status: domain.HoldApproved},
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	cleaned, err := achp.BuildCleaned(fixed, held, s.deps.Policy)
	if err != nil {
		t.Fatalf("cleaned: %v", err)
	}
	release, legs, _, err := achp.BuildRelease(fixed, held, s.deps.Policy)
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if legs != 1 {
		t.Fatalf("legs = %d, want 1", legs)
	}
	for kind, f := range map[domain.ArtifactKind]*ach.File{
		domain.ArtifactOriginal: fixed,
		domain.ArtifactFixed:    fixed,
		domain.ArtifactCleaned:  cleaned,
		domain.ArtifactRelease:  release,
	} {
		bytes, err := achp.Write(f)
		if err != nil {
			t.Fatalf("write %s: %v", kind, err)
		}
		sum := checksum.Bytes(bytes)
		if _, err := st.CreateArtifact(ctx, domain.Artifact{SubmissionID: sub.ID, Kind: kind, Checksum: sum, State: domain.ArtifactPublished}); err != nil {
			t.Fatalf("artifact %s: %v", kind, err)
		}
		if err := s.deps.Files.Put(ctx, sum, bytes); err != nil {
			t.Fatalf("put %s: %v", kind, err)
		}
	}

	var v submissionVerify
	rr := get(t, s, "/api/submissions/"+sub.ID.String()+"/verify")
	if rr.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !v.Verified {
		t.Fatalf("verified = false, issues: %v", v.Issues)
	}
	if len(v.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(v.Entries))
	}
	heldRow, freeRow := v.Entries[0], v.Entries[1]
	if heldRow.Sender != "12104288" || heldRow.SenderName != "Acme Corp" {
		t.Fatalf("sender = %q/%q, want 12104288/Acme Corp", heldRow.Sender, heldRow.SenderName)
	}
	if heldRow.OriginalAcct != "111111" || heldRow.FixedAcct != "111111" {
		t.Fatalf("held row accounts wrong: %+v", heldRow)
	}
	if heldRow.CleanedAcct != "555555" {
		t.Fatalf("cleaned acct = %q, want holding account 555555", heldRow.CleanedAcct)
	}
	if heldRow.ReleaseAcct != "111111" {
		t.Fatalf("release acct = %q, want 111111", heldRow.ReleaseAcct)
	}
	if freeRow.CleanedAcct != "222222" || freeRow.ReleaseAcct != "" {
		t.Fatalf("free row accounts wrong: %+v", freeRow)
	}
}

func TestSubmissionsFilteredPaged(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	for i, f := range []string{"alpha.ach", "bravo.ach", "charlie.ach"} {
		status := domain.SubmissionReady
		if i == 2 {
			status = domain.SubmissionArchived
		}
		if _, err := st.CreateSubmission(ctx, domain.Submission{
			Filename: f, Status: status, ReceivedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	// Search narrows to a single file.
	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/submissions?q=bravo", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("search = %d", rr.Code)
	}
	var page submissionsPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 1 || len(page.Submissions) != 1 || page.Submissions[0].Filename != "bravo.ach" {
		t.Fatalf("search page = %+v", page)
	}

	// Status filter.
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/submissions?status=archived", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 1 || page.Submissions[0].Filename != "charlie.ach" {
		t.Fatalf("status page = %+v", page)
	}

	// Pagination: page size 1 returns one row but total 3.
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/submissions?pageSize=1", nil))
	json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 3 || len(page.Submissions) != 1 {
		t.Fatalf("paged = %+v", page)
	}
}

func TestEventsFiltered(t *testing.T) {
	s, st := testServer(t)
	ctx := context.Background()
	evTypes := []string{"hold_created", "velocity_crossed", "hold_created"}
	for i, typ := range evTypes {
		if err := st.AppendEvent(ctx, typ, nil, nil); err != nil {
			t.Fatalf("event: %v", err)
		}
		_ = i
	}

	rr := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, httptest.NewRequest("GET", "/api/events?q=velocity", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("events = %d", rr.Code)
	}
	var page eventsPage
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if page.Total != 1 || len(page.Events) != 1 || page.Events[0].Type != "velocity_crossed" {
		t.Fatalf("events page = %+v", page)
	}
}
