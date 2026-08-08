package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/27actions/ach/internal/auth"
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
	rr.HeaderMap.Add("Cookie", auth.OAuthCookieName+"="+oauth)
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

	// Everyone can view the dashboard and holds.
	for _, sess := range []string{watcher, processor, admin} {
		for _, p := range []string{"/api/dashboard", "/api/holds"} {
			rr := httptest.NewRecorder()
			s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", p, nil), sess))
			if rr.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", p, rr.Code)
			}
		}
	}

	// Watchers may not approve/decline, and only admins see files/events.
	for _, p := range []string{"/api/submissions", "/api/events"} {
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

	// Processors review holds but cannot see files/events.
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/holds/"+holdID.String()+"/approve", strings.NewReader(`{}`)), processor))
	if rr.Code != http.StatusOK {
		t.Fatalf("processor approve = %d %s, want 200", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", "/api/submissions", nil), processor))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("processor submissions = %d, want 403", rr.Code)
	}

	// Admins do everything.
	adminHold := seedHold(t, st)
	rr = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("POST", "/api/holds/"+adminHold.String()+"/decline", strings.NewReader(`{}`)), admin))
	if rr.Code != http.StatusOK {
		t.Fatalf("admin decline = %d, want 200", rr.Code)
	}
	for _, p := range []string{"/api/submissions", "/api/entries", "/api/headers", "/api/events"} {
		rr := httptest.NewRecorder()
		s.Handler().ServeHTTP(rr, withCookie(httptest.NewRequest("GET", p, nil), admin))
		if rr.Code != http.StatusOK {
			t.Fatalf("admin %s = %d, want 200", p, rr.Code)
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
