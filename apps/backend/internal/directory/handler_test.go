package directory

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeDirectoryRepository struct {
	created Directory
}

func (fakeDirectoryRepository) ListByUser(context.Context, string, *string) ([]Directory, error) {
	return []Directory{}, nil
}

func (r *fakeDirectoryRepository) Create(_ context.Context, userID, name string, parentID *string) (Directory, error) {
	r.created = Directory{ID: "00000000-0000-4000-8000-000000000002", UserID: userID, Name: name, ParentID: parentID, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return r.created, nil
}

func (r *fakeDirectoryRepository) Rename(_ context.Context, id, userID, name string) (Directory, error) {
	r.created = Directory{ID: id, UserID: userID, Name: name, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	return r.created, nil
}

func (fakeDirectoryRepository) DeleteEmpty(context.Context, string, string) error { return nil }

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

func TestHandlerCreateDirectory(t *testing.T) {
	repository := &fakeDirectoryRepository{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/directories", strings.NewReader(`{"name":" Photos "}`))
	NewHandler(repository, "00000000-0000-4000-8000-000000000001").Create(recorder, request)
	if recorder.Code != 201 || !strings.Contains(recorder.Body.String(), `"name":"Photos"`) || strings.Contains(recorder.Body.String(), `"Name"`) {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerCreateRejectsInvalidParent(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/directories", strings.NewReader(`{"name":"Photos","parentId":"bad"}`))
	NewHandler(&fakeDirectoryRepository{}, "00000000-0000-4000-8000-000000000001").Create(recorder, request)
	if recorder.Code != 400 || !strings.Contains(recorder.Body.String(), "parentId") {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerRenameDirectory(t *testing.T) {
	repository := &fakeDirectoryRepository{}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("PATCH", "/api/v1/directories/00000000-0000-4000-8000-000000000002", strings.NewReader(`{"name":"Pictures"}`))
	NewHandler(repository, "00000000-0000-4000-8000-000000000001").Rename(recorder, request)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"name":"Pictures"`) {
		t.Fatalf("status/body: %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestHandlerDeleteDirectory(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("DELETE", "/api/v1/directories/00000000-0000-4000-8000-000000000002", nil)
	NewHandler(&fakeDirectoryRepository{}, "00000000-0000-4000-8000-000000000001").Delete(recorder, request)
	if recorder.Code != 204 {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}
