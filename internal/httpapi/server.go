// Package httpapi serves the review web UI's JSON API over the pipeline. It is
// a thin transport: reads go through ports.Store and the approve/decline
// actions delegate to the pipeline stages, so no business logic lives here.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/27actions/ach/internal/achp"
	"github.com/27actions/ach/internal/auth"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/pipeline"

	"github.com/google/uuid"
	"github.com/moov-io/ach"
)

// Server serves the JSON API and the built SPA.
type Server struct {
	deps      pipeline.Deps
	staticDir string
	// Auth is nil when auth is disabled (local dev/e2e), in which case the
	// actor falls back to the X-Actor header.
	entra        auth.Entra
	sessions     *auth.SessionManager
	cookieSecure bool
}

func New(d pipeline.Deps) *Server {
	return &Server{
		deps:      d,
		staticDir: "web/dist",
	}
}

// EnableAuth turns on Entra session auth for the API. It must be called before
// Handler.
func (s *Server) EnableAuth(entra auth.Entra, sessions *auth.SessionManager, cookieSecure bool) {
	s.entra = entra
	s.sessions = sessions
	s.cookieSecure = cookieSecure
}

// Handler builds the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	if s.sessions != nil {
		mux.HandleFunc("GET /api/auth/login", s.handleLogin)
		mux.HandleFunc("GET /api/auth/callback", s.handleCallback)
		mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
		mux.HandleFunc("GET /api/auth/me", s.handleMe)
	}
	mux.HandleFunc("GET /api/dashboard", s.handleDashboard)
	mux.HandleFunc("GET /api/holds", s.handleListHolds)
	mux.HandleFunc("GET /api/holds/{id}", s.handleGetHold)
	mux.HandleFunc("POST /api/holds/{id}/approve", s.handleApprove)
	mux.HandleFunc("POST /api/holds/{id}/decline", s.handleDecline)
	mux.HandleFunc("POST /api/holds/bulk", s.handleBulk)
	mux.HandleFunc("GET /api/submissions", s.handleListSubmissions)
	mux.HandleFunc("GET /api/submissions/{id}", s.handleGetSubmission)
	mux.HandleFunc("GET /api/submissions/{id}/verify", s.handleVerifySubmission)
	mux.HandleFunc("GET /api/entries", s.handleListEntries)
	mux.HandleFunc("GET /api/headers", s.handleListHeaders)
	mux.HandleFunc("GET /api/artifacts/{id}/content", s.handleArtifactContent)
	mux.HandleFunc("GET /api/events", s.handleEvents)
	mux.HandleFunc("/api/", http.NotFound)
	mux.Handle("/", s.spaHandler())
	return logRequests(s.requireAPI(mux))
}

// requireAPI guards the /api/* routes (but not /api/auth/* or /healthz) with a
// valid session when auth is enabled, and rejects cross-origin mutations.
func (s *Server) requireAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if s.sessions != nil && strings.HasPrefix(p, "/api/") &&
			!strings.HasPrefix(p, "/api/auth/") && p != "/healthz" {
			u := s.user(r)
			if u == nil {
				writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
				return
			}
			if !s.authorized(u, r.Method, p) {
				writeErr(w, http.StatusForbidden, errors.New("forbidden"))
				return
			}
		}
		if s.sessions != nil && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if origin := r.Header.Get("Origin"); origin != "" {
				if o, err := url.Parse(origin); err != nil || o.Host != r.Host {
					writeErr(w, http.StatusForbidden, errors.New("forbidden origin"))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// authorized enforces the role matrix: everyone may view the dashboard and
// holds; watchers may not review; only admins may see files/entries/headers/
// events/artifacts. Unknown roles are treated as watchers (least privilege).
func (s *Server) authorized(u *domain.User, method, path string) bool {
	if u.Role == domain.RoleAdmin {
		return true
	}
	switch {
	case path == "/api/dashboard":
		return true
	case strings.HasPrefix(path, "/api/holds"):
		if method != http.MethodGet {
			return u.Role == domain.RoleProcessor
		}
		return true
	default:
		// submissions, entries, headers, events, artifacts, verify: admin only.
		return false
	}
}

// user resolves the authenticated user from the session cookie, or nil.
func (s *Server) user(r *http.Request) *domain.User {
	if s.sessions == nil {
		return nil
	}
	c, err := r.Cookie(auth.SessionCookieName)
	if err != nil {
		return nil
	}
	u, err := s.sessions.Verify(r.Context(), c.Value)
	if err != nil {
		return nil
	}
	return &u
}

// actor resolves the reviewer identity for audit-trail entries: the signed-in
// user when auth is on, otherwise the X-Actor header (dev/e2e mode).
func (s *Server) actor(r *http.Request) string {
	if u := s.user(r); u != nil {
		if u.Name != "" {
			return u.Name
		}
		return u.UPN
	}
	if a := r.Header.Get("X-Actor"); a != "" {
		return a
	}
	return "web"
}

// spaHandler serves the built SPA with history routing: real files are served
// as-is, and any other non-/api path falls back to index.html so client-side
// routes deep-link cleanly.
func (s *Server) spaHandler() http.Handler {
	if _, err := os.Stat(s.staticDir); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "web UI not built (run: cd web && bun install && bun run build)", http.StatusNotFound)
		})
	}
	files := http.FileServer(http.Dir(s.staticDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := os.Stat(filepath.Join(s.staticDir, p)); err != nil {
			// Client-side route: serve the app shell.
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start))
	})
}

// --- responses ---

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

type holdDetail struct {
	domain.Hold
	Reviews      []domain.Review `json:"reviews"`
	Velocity     *velocityInfo   `json:"velocity,omitempty"`
	ReleaseState string          `json:"release_state"`
	GroupSize    int             `json:"group_size"`
}

type velocityInfo struct {
	Total int64 `json:"total"`
	Prior int64 `json:"prior"`
	Held  int64 `json:"held"`
}

type submissionDetail struct {
	domain.Submission
	Artifacts []domain.Artifact `json:"artifacts"`
	Holds     []domain.Hold     `json:"holds"`
	Jobs      []domain.Job      `json:"jobs"`
}

type reviewRequest struct {
	Note  string `json:"note"`
	Actor string `json:"actor"`
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- auth handlers ---

// handleLogin starts the Entra authorization-code + PKCE flow, stashing the
// state and verifier in a short-lived cookie bound to the browser.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.NewToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	verifier, err := auth.NewToken()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.OAuthCookieName,
		Value:    state + "|" + verifier,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
		MaxAge:   600,
	})
	http.Redirect(w, r, s.entra.LoginURL(state, verifier), http.StatusFound)
}

// handleCallback exchanges the code, upserts the user, and establishes the
// session cookie.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if q := r.URL.Query().Get("error"); q != "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("entra error: %s: %s", q, r.URL.Query().Get("error_description")))
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	c, err := r.Cookie(auth.OAuthCookieName)
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("missing oauth state"))
		return
	}
	parts := strings.SplitN(c.Value, "|", 2)
	if len(parts) != 2 || parts[0] != state {
		writeErr(w, http.StatusBadRequest, errors.New("oauth state mismatch"))
		return
	}

	id, err := s.entra.Exchange(r.Context(), code, parts[1])
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	user, err := s.deps.Store.UpsertUser(r.Context(), domain.User{
		Subject: id.Subject,
		UPN:     id.UPN,
		Email:   id.Email,
		Name:    id.Name,
		Role:    auth.MapRole(id.Roles),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	tok, err := s.sessions.Create(r.Context(), user.ID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cookieSecure,
		MaxAge:   int(s.sessions.TTL / time.Second),
	})
	http.SetCookie(w, &http.Cookie{
		Name: auth.OAuthCookieName, Value: "", Path: "/", HttpOnly: true, MaxAge: -1,
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

// handleLogout destroys the session and clears the cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.SessionCookieName); err == nil {
		_ = s.sessions.Destroy(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: auth.SessionCookieName, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleMe returns the signed-in user, or 401.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := s.user(r)
	if u == nil {
		writeErr(w, http.StatusUnauthorized, errors.New("unauthorized"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"name": u.Name, "upn": u.UPN, "email": u.Email, "role": u.Role,
	})
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
	rows, err := s.deps.Store.ListHoldsFiltered(ctx, status, search, start, end, 5000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	sortHolds(rows, q.Get("sort"), q.Get("dir"))
	writeJSON(w, http.StatusOK, holdsPage{Holds: slicePage(rows, page, pageSize), Total: total})
}

type bulkRequest struct {
	Action string      `json:"action"`
	IDs    []uuid.UUID `json:"ids"`
	Note   string      `json:"note"`
	Actor  string      `json:"actor"`
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
	actor := req.Actor
	if actor == "" {
		actor = s.actor(r)
	}
	var count int
	var err error
	switch req.Action {
	case "approve":
		count, err = pipeline.ApproveHolds(r.Context(), s.deps, req.IDs, actor, req.Note)
	case "decline":
		count, err = pipeline.DeclineHolds(r.Context(), s.deps, req.IDs, actor, req.Note)
	default:
		writeErr(w, http.StatusBadRequest, fmt.Errorf("unknown action %q", req.Action))
		return
	}
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": count})
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
	rows, err := s.deps.Store.ListEntriesFiltered(ctx, search, start, end, 5000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	sortEntries(rows, q.Get("sort"), q.Get("dir"))
	writeJSON(w, http.StatusOK, entriesPage{Entries: slicePage(rows, page, pageSize), Total: total})
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
	rows, err := s.deps.Store.ListHeadersFiltered(ctx, search, start, end, 5000)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	sortHeaders(rows, q.Get("sort"), q.Get("dir"))
	writeJSON(w, http.StatusOK, headersPage{Headers: slicePage(rows, page, pageSize), Total: total})
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

// --- pagination & sorting ---

type holdsPage struct {
	Holds []domain.Hold `json:"holds"`
	Total int64         `json:"total"`
}

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

func parseDate(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

func pageParams(r *http.Request) (page, size int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	size, _ = strconv.Atoi(r.URL.Query().Get("pageSize"))
	if size < 1 || size > 100 {
		size = 25
	}
	return page, size
}

func slicePage[T any](rows []T, page, size int) []T {
	start := (page - 1) * size
	if start >= len(rows) {
		return []T{}
	}
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

func sortHolds(rows []domain.Hold, col, dir string) {
	valid := map[string]bool{"amount": true, "status": true, "trace": true, "rdfi": true,
		"receiver": true, "account": true, "effective": true, "created": true, "filename": true}
	if !valid[col] {
		return // keep the SQL order (created_at DESC)
	}
	asc := dir != "desc"
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "amount":
			less = a.EntryAmount < b.EntryAmount
		case "status":
			less = a.Status < b.Status
		case "trace":
			less = a.EntryTrace < b.EntryTrace
		case "rdfi":
			less = a.EntryRdfi < b.EntryRdfi
		case "receiver":
			less = a.EntryReceiverName < b.EntryReceiverName
		case "account":
			less = a.EntryReceiverAcct < b.EntryReceiverAcct
		case "effective":
			less = effStr(a.EffectiveDate) < effStr(b.EffectiveDate)
		case "created":
			less = a.CreatedAt.Before(b.CreatedAt)
		default:
			less = a.Filename < b.Filename
		}
		if asc {
			return less
		}
		return !less
	})
}

func sortEntries(rows []domain.BatchEntry, col, dir string) {
	valid := map[string]bool{"amount": true, "tran_code": true, "trace": true, "rdfi": true,
		"receiver": true, "account": true, "effective": true, "filename": true}
	if !valid[col] {
		return
	}
	asc := dir != "desc"
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "amount":
			less = a.Amount < b.Amount
		case "tran_code":
			less = a.TranCode < b.TranCode
		case "trace":
			less = a.Trace < b.Trace
		case "rdfi":
			less = a.Rdfi < b.Rdfi
		case "receiver":
			less = a.ReceiverName < b.ReceiverName
		case "account":
			less = a.ReceiverAccount < b.ReceiverAccount
		case "effective":
			less = effStr(a.EffectiveDate) < effStr(b.EffectiveDate)
		default:
			less = a.Filename < b.Filename
		}
		if asc {
			return less
		}
		return !less
	})
}

func sortHeaders(rows []domain.BatchHeader, col, dir string) {
	valid := map[string]bool{"customer_id": true, "company_name": true, "effective": true, "filename": true}
	if !valid[col] {
		return
	}
	asc := dir != "desc"
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		var less bool
		switch col {
		case "customer_id":
			less = a.CustomerID < b.CustomerID
		case "company_name":
			less = a.CompanyName < b.CompanyName
		case "effective":
			less = effStr(a.EffectiveDate) < effStr(b.EffectiveDate)
		default:
			less = a.Filename < b.Filename
		}
		if asc {
			return less
		}
		return !less
	})
}

func effStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
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
	actor := req.Actor
	if actor == "" {
		actor = s.actor(r)
	}
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
	actor := req.Actor
	if actor == "" {
		actor = s.actor(r)
	}
	if err := pipeline.DeclineHold(r.Context(), s.deps, id, actor, req.Note); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "declined"})
}

func (s *Server) handleListSubmissions(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limit := intParam(r, "limit", 100, 500)
	subs, err := s.deps.Store.ListSubmissions(r.Context(), status, limit)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

func (s *Server) handleGetSubmission(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	sub, err := s.deps.Store.GetSubmission(ctx, id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err)
			return
		}
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	detail := submissionDetail{Submission: sub}
	if detail.Artifacts, err = s.deps.Store.ListArtifactsBySubmission(ctx, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if detail.Holds, err = s.deps.Store.ListHoldsBySubmission(ctx, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if detail.Jobs, err = s.deps.Store.ListJobsByRef(ctx, id); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	page, pageSize := pageParams(r)
	ctx := r.Context()
	total, err := s.deps.Store.CountEvents(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	events, err := s.deps.Store.ListEvents(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, eventsPage{Events: events, Total: total})
}

// --- submission verification ---

type artifactSum struct {
	Present bool   `json:"present"`
	Entries int    `json:"entries"`
	Total   int64  `json:"total"`
	Error   string `json:"error,omitempty"`
}

type entryMatch struct {
	Trace        string `json:"trace"`
	Amount       int64  `json:"amount"`
	OriginalAcct string `json:"original_account"`
	FixedAcct    string `json:"fixed_account"`
	CleanedAcct  string `json:"cleaned_account"`
	ReceiverKept bool   `json:"receiver_kept"`
}

type submissionVerify struct {
	Verified  bool                   `json:"verified"`
	Artifacts map[string]artifactSum `json:"artifacts"`
	Issues    []string               `json:"issues"`
	Entries   []entryMatch           `json:"entries"`
}

// handleVerifySubmission re-parses a submission's artifacts and checks that
// amounts and receiver accounts hold across the original → fixed → cleaned
// chain, surfacing any mismatch for review.
func (s *Server) handleVerifySubmission(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ctx := r.Context()
	arts, err := s.deps.Store.ListArtifactsBySubmission(ctx, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}

	sums := map[string]artifactSum{}
	files := map[domain.ArtifactKind]*ach.File{}
	for _, a := range arts {
		sum := artifactSum{Present: true}
		data, err := s.deps.Files.Get(ctx, a.Checksum)
		if err != nil {
			sum.Error = err.Error()
			sums[string(a.Kind)] = sum
			continue
		}
		f, err := achp.Read(data, true)
		if err != nil {
			sum.Error = err.Error()
			sums[string(a.Kind)] = sum
			continue
		}
		sum.Entries = achp.TotalEntries(f)
		sum.Total = artifactTotal(f)
		sums[string(a.Kind)] = sum
		files[a.Kind] = f
	}

	orig, hasOrig := files[domain.ArtifactOriginal]
	fixed, hasFixed := files[domain.ArtifactFixed]
	cleaned, hasCleaned := files[domain.ArtifactCleaned]

	var issues []string
	if hasOrig && hasFixed {
		if err := achp.VerifySame(orig, fixed); err != nil {
			issues = append(issues, "fixed does not match original: "+err.Error())
		}
	}
	if hasFixed && hasCleaned {
		if err := amountsMatch(fixed, cleaned); err != nil {
			issues = append(issues, "cleaned amounts do not match fixed: "+err.Error())
		}
	}
	if hasCleaned && !hasFixed {
		issues = append(issues, "cleaned artifact exists without a fixed artifact")
	}
	if !hasCleaned && hasFixed {
		issues = append(issues, "submission has not been processed (no cleaned artifact)")
	}

	var entries []entryMatch
	if hasOrig && hasFixed {
		entries = compareEntries(orig, fixed, cleaned)
	}
	if issues == nil {
		issues = []string{}
	}
	if entries == nil {
		entries = []entryMatch{}
	}

	writeJSON(w, http.StatusOK, submissionVerify{
		Verified:  len(issues) == 0 && hasCleaned,
		Artifacts: sums,
		Issues:    issues,
		Entries:   entries,
	})
}

func artifactTotal(f *ach.File) int64 {
	var total int64
	for _, b := range f.Batches {
		for _, e := range b.GetEntries() {
			total += int64(e.Amount)
		}
	}
	return total
}

func amountsMatch(fixed, cleaned *ach.File) error {
	if len(cleaned.Batches) != len(fixed.Batches) {
		return fmt.Errorf("batch count %d != %d", len(cleaned.Batches), len(fixed.Batches))
	}
	for bi := range fixed.Batches {
		fe, ce := fixed.Batches[bi].GetEntries(), cleaned.Batches[bi].GetEntries()
		if len(fe) != len(ce) {
			return fmt.Errorf("batch %d entry count %d != %d", bi+1, len(ce), len(fe))
		}
		for ei := range fe {
			if fe[ei].Amount != ce[ei].Amount {
				return fmt.Errorf("batch %d entry %d amount %d != %d", bi+1, ei+1, ce[ei].Amount, fe[ei].Amount)
			}
		}
	}
	return nil
}

func compareEntries(orig, fixed, cleaned *ach.File) []entryMatch {
	var out []entryMatch
	for bi, fb := range fixed.Batches {
		if bi >= len(orig.Batches) {
			break
		}
		fe := fb.GetEntries()
		oe := orig.Batches[bi].GetEntries()
		var ce []*ach.EntryDetail
		if cleaned != nil && bi < len(cleaned.Batches) {
			ce = cleaned.Batches[bi].GetEntries()
		}
		for ei := range fe {
			em := entryMatch{
				Trace:        fe[ei].TraceNumberField(),
				Amount:       int64(fe[ei].Amount),
				OriginalAcct: strings.TrimSpace(oe[ei].DFIAccountNumber),
				FixedAcct:    strings.TrimSpace(fe[ei].DFIAccountNumber),
				ReceiverKept: oe[ei].DFIAccountNumber == fe[ei].DFIAccountNumber,
			}
			if ei < len(ce) {
				em.CleanedAcct = strings.TrimSpace(ce[ei].DFIAccountNumber)
			}
			out = append(out, em)
		}
	}
	return out
}

// submissions, so a reviewer can see the total exposure to that account.
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

// --- helpers ---

func decodeReview(r *http.Request) (reviewRequest, error) {
	var req reviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return req, err
	}
	return req, nil
}

func intParam(r *http.Request, name string, def, max int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

func writeErr(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf("%v", err)})
}
