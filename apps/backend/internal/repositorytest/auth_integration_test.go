package repositorytest

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
)

func TestPostgresSessionsExpireAndRevoke(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var userID string
	if err := pool.QueryRow(ctx, "INSERT INTO users (email, password_hash) VALUES ('auth@example.test', 'unused') RETURNING id").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	token, digest, err := auth.NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	repo := auth.NewSessionRepository(pool)
	if _, err := repo.Create(ctx, userID, digest, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if session, err := repo.GetByTokenHash(ctx, auth.HashSessionToken(token), time.Now()); err != nil || session.UserID != userID {
		t.Fatalf("lookup = %+v, %v", session, err)
	}
	if err := repo.Delete(ctx, digest); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByTokenHash(ctx, digest, time.Now()); err == nil {
		t.Fatal("revoked session unexpectedly found")
	}
}

func TestPostgresAuthHandlerCookieFlow(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hash, err := auth.HashPassword("integration-secret")
	if err != nil {
		t.Fatal(err)
	}
	var userID string
	if err := pool.QueryRow(ctx, "INSERT INTO users (email, password_hash) VALUES ('login@example.test', $1) RETURNING id", hash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	handler := auth.NewHandler(auth.NewUserRepository(pool), auth.NewSessionRepository(pool), time.Hour)
	invalid := httptest.NewRecorder()
	handler.Login(invalid, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"login@example.test","password":"wrong"}`)))
	if invalid.Code != 401 || len(invalid.Result().Cookies()) != 0 || !strings.Contains(invalid.Body.String(), "invalid_credentials") {
		t.Fatalf("invalid login status/body/cookies: %d %s %+v", invalid.Code, invalid.Body.String(), invalid.Result().Cookies())
	}
	login := httptest.NewRecorder()
	handler.Login(login, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"login@example.test","password":"integration-secret"}`)))
	if login.Code != 200 {
		t.Fatalf("login status/body: %d %s", login.Code, login.Body.String())
	}
	cookies := login.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("login cookie = %+v", cookies)
	}
	meRequest := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	meRequest.AddCookie(cookies[0])
	me := httptest.NewRecorder()
	handler.Me(me, meRequest)
	if me.Code != 200 || !strings.Contains(me.Body.String(), userID) {
		t.Fatalf("me status/body: %d %s", me.Code, me.Body.String())
	}
	logoutRequest := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	logoutRequest.AddCookie(cookies[0])
	logout := httptest.NewRecorder()
	handler.Logout(logout, logoutRequest)
	if logout.Code != 204 {
		t.Fatalf("logout status: %d", logout.Code)
	}
	meAfter := httptest.NewRecorder()
	handler.Me(meAfter, meRequest)
	if meAfter.Code != 401 {
		t.Fatalf("me after logout status/body: %d %s", meAfter.Code, meAfter.Body.String())
	}
}

func TestPostgresLoginAttemptWindowAndReset(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	repo := auth.NewAttemptRepository(pool)
	first, err := repo.RecordFailure(ctx, " User@Example.Test ", time.Hour, time.Hour, 3)
	if err != nil || first.FailedCount != 1 || first.Email != "user@example.test" {
		t.Fatalf("first attempt = %+v, %v", first, err)
	}
	second, err := repo.RecordFailure(ctx, "user@example.test", time.Hour, time.Hour, 2)
	if err != nil || second.FailedCount != 2 || second.LockedUntil == nil {
		t.Fatalf("second attempt = %+v, %v", second, err)
	}
	if err := repo.Reset(ctx, "USER@example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordFailure(ctx, "user@example.test", time.Hour, time.Hour, 3); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresAuthHandlerEnforcesLockout(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	hash, err := auth.HashPassword("lockout-secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO users (email, password_hash) VALUES ('lockout@example.test', $1)", hash); err != nil {
		t.Fatal(err)
	}
	handler := auth.NewHandler(auth.NewUserRepository(pool), auth.NewSessionRepository(pool), time.Hour, auth.NewAttemptRepository(pool))
	for attempt := 0; attempt < 5; attempt++ {
		recorder := httptest.NewRecorder()
		handler.Login(recorder, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"lockout@example.test","password":"wrong"}`)))
		if recorder.Code != 401 {
			t.Fatalf("failed login %d status = %d", attempt+1, recorder.Code)
		}
	}
	recorder := httptest.NewRecorder()
	handler.Login(recorder, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"lockout@example.test","password":"lockout-secret"}`)))
	if recorder.Code != 401 || len(recorder.Result().Cookies()) != 0 {
		t.Fatalf("locked valid login status/cookies: %d %+v", recorder.Code, recorder.Result().Cookies())
	}
}
