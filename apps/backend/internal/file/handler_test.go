package file

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeFileRepository struct {
	files  []File
	userID string
}

func (f fakeFileRepository) ListByUser(_ context.Context, userID string) ([]File, error) {
	f.userID = userID
	return f.files, nil
}

func (fakeFileRepository) Rename(_ context.Context, id, userID, name string) (File, error) {
	return File{ID: id, UserID: userID, Name: name, UpdatedAt: time.Now()}, nil
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

func TestHandlerRenameFile(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("PATCH", "/api/v1/files/00000000-0000-4000-8000-000000000002", strings.NewReader(`{"name":"renamed.txt"}`))
	NewHandler(fakeFileRepository{}, "00000000-0000-4000-8000-000000000001").Rename(recorder, request)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"name":"renamed.txt"`) {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}
