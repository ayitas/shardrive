package file

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeFileRepository struct {
	files  []File
	userID string
}

func (f fakeFileRepository) ListByUser(_ context.Context, userID string) ([]File, error) {
	f.userID = userID
	return f.files, nil
}

func TestHandlerListRequiresConfiguredUser(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/files", nil)
	NewHandler(fakeFileRepository{}, "").List(recorder, request)
	if recorder.Code != 503 || !strings.Contains(recorder.Body.String(), "not_configured") {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerListRejectsInvalidUser(t *testing.T) {
	recorder := httptest.NewRecorder()
	NewHandler(fakeFileRepository{}, "not-a-uuid").List(recorder, httptest.NewRequest("GET", "/api/v1/files", nil))
	if recorder.Code != 503 {
		t.Fatalf("status = %d", recorder.Code)
	}
}
