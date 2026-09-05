package directory

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeDirectoryRepository struct{}

func (fakeDirectoryRepository) ListByUser(context.Context, string, *string) ([]Directory, error) {
	return []Directory{}, nil
}

func TestHandlerListRejectsInvalidParent(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/directories?parentId=bad", nil)
	NewHandler(fakeDirectoryRepository{}, "00000000-0000-4000-8000-000000000001").List(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "parentId") {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerListRootReturnsArray(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("GET", "/api/v1/directories", nil)
	NewHandler(fakeDirectoryRepository{}, "00000000-0000-4000-8000-000000000001").List(recorder, request)
	if recorder.Code != 200 || recorder.Body.String() == "" {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}
