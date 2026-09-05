package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginFailsClosedWhenUnconfigured(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"a@example.test","password":"secret"}`))
	(&Handler{}).Login(recorder, request)
	if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), "not_configured") {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestLogoutClearsSecureCookie(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHandler(nil, nil, time.Hour).Logout(recorder, httptest.NewRequest("POST", "/api/v1/auth/logout", nil))
	if recorder.Code != 204 {
		t.Fatalf("status = %d", recorder.Code)
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionCookieName || cookies[0].MaxAge > 0 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", cookies)
	}
}
