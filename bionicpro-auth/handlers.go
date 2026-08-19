package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
)

type contextKey string

const ctxSessionID contextKey = "sessionID"
const ctxAccessToken contextKey = "accessToken"

type Server struct {
	cfg        Config
	kc         *KeycloakClient
	pkce       *PKCEStore
	sessions   *SessionStore
	httpClient *http.Client
}

func NewServer(cfg Config) *Server {
	return &Server{
		cfg:        cfg,
		kc:         NewKeycloakClient(cfg.KeycloakInternalURL, cfg.Realm, cfg.ClientID),
		pkce:       NewPKCEStore(),
		sessions:   NewSessionStore(),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *Server) callbackURL() string {
	return s.cfg.BackendPublicURL + "/auth/callback"
}

func (s *Server) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", s.cfg.FrontendURL)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.setCORSHeaders(w, r)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleLogin starts the PKCE authorization code flow: it never hands a
// token to the browser, only a redirect to Keycloak carrying a
// code_challenge whose verifier stays server-side.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, challenge := s.pkce.NewChallenge()
	dest := AuthorizeURL(s.cfg.KeycloakPublicURL, s.cfg.Realm, s.cfg.ClientID, s.callbackURL(), state, challenge)
	http.Redirect(w, r, dest, http.StatusFound)
}

// handleCallback exchanges the authorization code (+ PKCE verifier) for
// tokens, stores them server-side bound to a new session, and hands the
// browser only an HttpOnly+Secure session cookie.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if errParam := q.Get("error"); errParam != "" {
		http.Error(w, "authentication failed: "+errParam, http.StatusUnauthorized)
		return
	}
	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	verifier, err := s.pkce.Consume(state)
	if err != nil {
		http.Error(w, "invalid or expired state", http.StatusBadRequest)
		return
	}

	tr, err := s.kc.ExchangeCode(code, verifier, s.callbackURL())
	if err != nil {
		log.Printf("token exchange failed: %v", err)
		http.Error(w, "token exchange failed", http.StatusBadGateway)
		return
	}

	subject, err := subjectFromAccessToken(tr.AccessToken)
	if err != nil {
		log.Printf("cannot parse access token: %v", err)
	}

	accessExpiresAt := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	sessionTTL := time.Duration(s.cfg.SessionTTLSeconds) * time.Second
	if sessionTTL <= time.Duration(tr.ExpiresIn)*time.Second {
		// session must outlive the access token so a refresh is possible
		sessionTTL = time.Duration(tr.ExpiresIn)*time.Second + 5*time.Minute
	}

	sessionID := s.sessions.Create(subject, tr.AccessToken, tr.RefreshToken, accessExpiresAt, sessionTTL)
	s.setSessionCookie(w, sessionID, sessionTTL)

	http.Redirect(w, r, s.cfg.FrontendURL, http.StatusFound)
}

func (s *Server) setSessionCookie(w http.ResponseWriter, sessionID string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// requireSession is the auth middleware for every protected resource. On
// each successful call it: (1) transparently refreshes the access token via
// refresh_token if expired, and (2) rotates the session id so a cookie
// value observed once is worthless on the next request (session-fixation
// mitigation), as required by the task.
func (s *Server) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cfg.CookieName)
		if err != nil {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		sessionID := cookie.Value

		subject, accessToken, refreshToken, accessExpiresAt, err := s.sessions.Get(sessionID)
		if err != nil {
			s.clearSessionCookie(w)
			if errors.Is(err, errSessionExpired) {
				http.Error(w, "session expired", http.StatusUnauthorized)
			} else {
				http.Error(w, "not authenticated", http.StatusUnauthorized)
			}
			return
		}

		if time.Now().After(accessExpiresAt) {
			tr, err := s.kc.RefreshTokens(refreshToken)
			if err != nil {
				log.Printf("refresh failed for session: %v", err)
				s.sessions.Delete(sessionID)
				s.clearSessionCookie(w)
				http.Error(w, "session expired", http.StatusUnauthorized)
				return
			}
			accessToken = tr.AccessToken
			refreshToken = tr.RefreshToken
			accessExpiresAt = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
			if err := s.sessions.UpdateTokens(sessionID, accessToken, refreshToken, accessExpiresAt); err != nil {
				http.Error(w, "not authenticated", http.StatusUnauthorized)
				return
			}
		}

		newSessionID, err := s.sessions.Rotate(sessionID)
		if err != nil {
			http.Error(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		ttl := s.sessions.TTLFor(newSessionID)
		s.setSessionCookie(w, newSessionID, ttl)
		w.Header().Set("X-Session-Id", newSessionID)

		ctx := context.WithValue(r.Context(), ctxSessionID, newSessionID)
		ctx = context.WithValue(ctx, ctxAccessToken, accessToken)
		_ = subject
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

func (s *Server) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"authenticated": true})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(s.cfg.CookieName)
	if err == nil {
		_, _, refreshToken, _, getErr := s.sessions.Get(cookie.Value)
		if getErr == nil {
			_ = s.kc.Logout(refreshToken)
		}
		s.sessions.Delete(cookie.Value)
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleReports proxies to the reports API using the server-held access
// token; the browser never sees it. If REPORTS_API_URL isn't configured
// (that service is built in a later task) it returns a stub so the auth
// flow itself can still be exercised end-to-end.
func (s *Server) handleReports(w http.ResponseWriter, r *http.Request) {
	accessToken, _ := r.Context().Value(ctxAccessToken).(string)

	if s.cfg.ReportsAPIURL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status": "reports-api not configured, bionicpro-auth reached this far authenticated",
		})
		return
	}

	req, err := http.NewRequest(http.MethodGet, s.cfg.ReportsAPIURL+"/reports", nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		http.Error(w, "reports API unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}
