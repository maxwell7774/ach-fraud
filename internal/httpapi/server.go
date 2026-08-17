// Package httpapi serves the review web UI's JSON API over the pipeline. It is
// a thin transport: reads go through ports.Store and the approve/decline
// actions delegate to the pipeline stages, so no business logic lives here.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/27actions/ach/internal/auth"
	"github.com/27actions/ach/internal/domain"
	"github.com/27actions/ach/internal/pipeline"
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
	staticDir := "web/dist"
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "web", "dist")
		if _, err := os.Stat(candidate); err == nil {
			staticDir = candidate
		}
	}
	return &Server{
		deps:      d,
		staticDir: staticDir,
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
	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("POST /api/jobs/{id}/requeue", s.handleRequeueJob)
	mux.HandleFunc("GET /api/recipients", s.handleListRecipients)
	mux.HandleFunc("POST /api/recipients", s.handleCreateRecipient)
	mux.HandleFunc("PUT /api/recipients/{id}", s.handleUpdateRecipient)
	mux.HandleFunc("DELETE /api/recipients/{id}", s.handleDeleteRecipient)
	mux.HandleFunc("/api/", http.NotFound)
	mux.Handle("/", s.spaHandler())
	return logRequests(s.requireAPI(mux))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		if r.Body != nil && r.Method != http.MethodGet && r.Method != http.MethodHead {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		}
		next.ServeHTTP(w, r)
	})
}

// authorized enforces the role matrix: everyone may view the dashboard and
// holds; watchers may not review; admins see files/entries/headers/artifacts;
// only super admins see events, manage email alert recipients, and requeue
// failed jobs. Unknown roles are treated as watchers (least privilege).
func (s *Server) authorized(u *domain.User, method, path string) bool {
	if u.Role == domain.RoleSuperAdmin {
		return true
	}
	switch {
	case path == "/api/dashboard":
		return true
	case strings.HasPrefix(path, "/api/holds"):
		if method != http.MethodGet {
			return u.Role == domain.RoleProcessor || u.Role == domain.RoleAdmin
		}
		return true
	case path == "/api/events" || strings.HasPrefix(path, "/api/recipients") || strings.HasPrefix(path, "/api/jobs"):
		// Events, email-recipient management, and job requeue are super-admin only.
		return false
	default:
		// submissions, entries, headers, artifacts, verify: admin only.
		return u.Role == domain.RoleAdmin
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

// reviewActor resolves the reviewer identity for an audit action. With auth
// enabled the signed-in user is authoritative — anything the client sent is
// ignored, so a caller cannot spoof the reviewer. In dev mode the request body
// actor (or X-Actor header) is used.
func (s *Server) reviewActor(r *http.Request, bodyActor string) string {
	if s.sessions != nil {
		return s.actor(r)
	}
	if bodyActor != "" {
		return bodyActor
	}
	return s.actor(r)
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

// --- request & response helpers ---

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

// pageSlice returns the (page, size) window of rows, clamped to the slice.
func pageSlice[T any](rows []T, page, size int) []T {
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
