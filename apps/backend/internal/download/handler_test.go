package download

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
)

func TestHandlerStreamsWithSafeHeaders(t *testing.T) {
	checksum := strings.Repeat("a", 64)
	service := &stubService{plan: Plan{File: filedomain.File{
		ID: "33333333-3333-4333-8333-333333333333", Name: "evil\"\r\nX-Bad: yes.txt",
		MIMEType: "text/plain\r\nX-Bad: yes", SizeBytes: 3, State: filedomain.StateAvailable,
		ChecksumSHA256: &checksum,
	}}, body: "abc"}
	handler := NewHandler(service, "11111111-1111-4111-8111-111111111111")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/files/33333333-3333-4333-8333-333333333333/download", nil)
	request.SetPathValue("id", "33333333-3333-4333-8333-333333333333")
	response := httptest.NewRecorder()
	handler.Download(response, request)
	if response.Code != http.StatusOK || response.Body.String() != "abc" {
		t.Fatalf("response = %d %q", response.Code, response.Body.String())
	}
	if response.Header().Get("Content-Length") != "3" || response.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("length/type headers = %q / %q", response.Header().Get("Content-Length"), response.Header().Get("Content-Type"))
	}
	disposition := response.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment") || strings.ContainsAny(disposition, "\r\n") {
		t.Fatalf("unsafe Content-Disposition = %q", disposition)
	}
	if response.Header().Get("X-Bad") != "" || response.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("unexpected security headers = %#v", response.Header())
	}
}

func TestHandlerErrorsBeforeStreaming(t *testing.T) {
	service := &stubService{prepareErr: domain.ErrNotFound}
	handler := NewHandler(service, "11111111-1111-4111-8111-111111111111")
	request := httptest.NewRequest(http.MethodGet, "/api/v1/files/id/download", nil)
	request.SetPathValue("id", "id")
	response := httptest.NewRecorder()
	handler.Download(response, request)
	if response.Code != http.StatusNotFound || service.streamed {
		t.Fatalf("error response = %d, streamed=%v", response.Code, service.streamed)
	}

	handler = NewHandler(&stubService{}, "")
	response = httptest.NewRecorder()
	handler.Download(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured response = %d", response.Code)
	}
}

func TestHandlerErrorMapping(t *testing.T) {
	for _, test := range []struct {
		err  error
		want int
	}{
		{domain.ErrInvalid, http.StatusBadRequest},
		{domain.ErrInvalidState, http.StatusConflict},
		{ErrIntegrity, http.StatusConflict},
		{errors.New("database"), http.StatusInternalServerError},
	} {
		response := httptest.NewRecorder()
		writeServiceError(response, test.err)
		if response.Code != test.want {
			t.Errorf("error %v status = %d, want %d", test.err, response.Code, test.want)
		}
	}
}

type stubService struct {
	plan       Plan
	prepareErr error
	body       string
	streamErr  error
	streamed   bool
}

func (s *stubService) Prepare(context.Context, string, string) (Plan, error) {
	return s.plan, s.prepareErr
}

func (s *stubService) Stream(_ context.Context, _ Plan, destination io.Writer) error {
	s.streamed = true
	if s.body != "" {
		_, _ = io.WriteString(destination, s.body)
	}
	return s.streamErr
}
