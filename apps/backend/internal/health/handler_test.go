package health

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type stubPinger struct{ err error }

func (p stubPinger) Ping(context.Context) error { return p.err }

func TestLive(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHandler(stubPinger{}, time.Second).Live(recorder, httptest.NewRequest(http.MethodGet, "/health/live", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestReady(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "database available", want: http.StatusOK},
		{name: "database unavailable", err: errors.New("offline"), want: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			NewHandler(stubPinger{err: tt.err}, time.Second).Ready(recorder, httptest.NewRequest(http.MethodGet, "/health/ready", nil))
			if recorder.Code != tt.want {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.want)
			}
		})
	}
}
