package httpapi

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/27actions/ach/internal/auth"
	"github.com/27actions/ach/internal/domain"
)

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
