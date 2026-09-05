package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type contextKey struct{}

func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, contextKey{}, userID)
}
func UserID(ctx context.Context) (string, bool) {
	value, ok := ctx.Value(contextKey{}).(string)
	return value, ok
}

const sessionCookieName = "shardrive_session"

type Handler struct {
	users    *UserRepository
	sessions *SessionRepository
	lifetime time.Duration
	attempts *AttemptRepository
	policy   AttemptPolicy
	secure   bool
}

type AttemptPolicy struct {
	Window, Lock time.Duration
	MaxFailures  int
}

func NewHandler(users *UserRepository, sessions *SessionRepository, lifetime time.Duration, attempts ...*AttemptRepository) *Handler {
	var repository *AttemptRepository
	if len(attempts) > 0 {
		repository = attempts[0]
	}
	return &Handler{users: users, sessions: sessions, lifetime: lifetime, attempts: repository, secure: true, policy: AttemptPolicy{Window: 15 * time.Minute, Lock: 15 * time.Minute, MaxFailures: 5}}
}

func (h *Handler) SetCookieSecure(secure bool) { h.secure = secure }

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	if h.users == nil || h.sessions == nil || h.lifetime <= 0 {
		writeAuthError(w, http.StatusServiceUnavailable, "not_configured", "authentication is not configured")
		return
	}
	var request loginRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.Email) == "" || request.Password == "" {
		writeAuthError(w, http.StatusBadRequest, "invalid_request", "email and password are required")
		return
	}
	if h.attempts != nil {
		locked, err := h.attempts.IsLocked(r.Context(), request.Email, time.Now())
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "internal_error", "authentication failed")
			return
		}
		if locked {
			writeAuthError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
			return
		}
	}
	user, err := h.users.GetByEmail(r.Context(), request.Email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			h.recordFailure(r, request.Email)
			writeAuthError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
			return
		}
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "authentication failed")
		return
	}
	ok, err := VerifyPassword(request.Password, user.PasswordHash)
	if err != nil || !ok {
		h.recordFailure(r, request.Email)
		writeAuthError(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	if h.attempts != nil {
		_ = h.attempts.Reset(r.Context(), request.Email)
	}
	token, digest, err := NewSessionToken()
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "authentication failed")
		return
	}
	if _, err := h.sessions.Create(r.Context(), user.ID, digest, time.Now().Add(h.lifetime)); err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "authentication failed")
		return
	}
	h.setCookie(w, token, h.lifetime)
	writeAuthJSON(w, http.StatusOK, map[string]string{"userId": user.ID, "email": user.Email})
}

func (h *Handler) recordFailure(r *http.Request, email string) {
	if h.attempts != nil {
		_, _ = h.attempts.RecordFailure(r.Context(), email, h.policy.Window, h.policy.Lock, h.policy.MaxFailures)
	}
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.sessions != nil {
		if cookie, err := r.Cookie(sessionCookieName); err == nil {
			_ = h.sessions.Delete(r.Context(), HashSessionToken(cookie.Value))
		}
	}
	h.setCookie(w, "", -time.Second)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	session, err := h.sessions.GetByTokenHash(r.Context(), HashSessionToken(cookie.Value), time.Now())
	if err != nil {
		writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	writeAuthJSON(w, http.StatusOK, map[string]string{"userId": session.UserID})
}

func (h *Handler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/logout" || r.URL.Path == "/api/v1/auth/me" || r.URL.Path == "/health/live" || r.URL.Path == "/health/ready" || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		session, err := h.sessions.GetByTokenHash(r.Context(), HashSessionToken(cookie.Value), time.Now())
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(WithUserID(r.Context(), session.UserID)))
	})
}

func (h *Handler) setCookie(w http.ResponseWriter, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: value, Path: "/", MaxAge: int(maxAge.Seconds()), HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
}
func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	writeAuthJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func writeAuthJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
