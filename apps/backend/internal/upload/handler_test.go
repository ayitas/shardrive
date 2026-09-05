package upload

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

const testUserID = "11111111-1111-4111-8111-111111111111"

func TestHandlerCreate(t *testing.T) {
	service := &stubUploadService{created: CreatedUpload{
		Session: Session{ID: "22222222-2222-4222-8222-222222222222"},
		File:    filedomain.File{ID: "33333333-3333-4333-8333-333333333333", ChunkSize: 32, ChunkCount: 3},
	}}
	handler := NewHandler(service, testUserID)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/uploads", strings.NewReader(`{"name":"payload.bin","size":65,"mimeType":"application/octet-stream"}`))
	response := httptest.NewRecorder()
	handler.Create(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body createHTTPResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.UploadID != service.created.Session.ID || body.FileID != service.created.File.ID || body.ChunkCount != 3 {
		t.Fatalf("response = %+v", body)
	}
	if service.createUserID != testUserID || service.createRequest.Name != "payload.bin" || service.createRequest.SizeBytes != 65 {
		t.Fatalf("service call = user %q request %+v", service.createUserID, service.createRequest)
	}
}

func TestHandlerRejectsMalformedBodies(t *testing.T) {
	handler := NewHandler(&stubUploadService{}, testUserID)
	for _, body := range []string{
		`{"name":"a","size":1,"unknown":true}`,
		`{"name":"a","size":1} {"name":"b","size":2}`,
		`{"name":`,
	} {
		response := httptest.NewRecorder()
		handler.Create(response, httptest.NewRequest(http.MethodPost, "/api/v1/uploads", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Errorf("body %q status = %d", body, response.Code)
		}
	}
}

func TestHandlerGetAndChunksAlwaysReturnArray(t *testing.T) {
	expires := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	service := &stubUploadService{status: Status{Session: Session{
		ID: "22222222-2222-4222-8222-222222222222", FileID: "33333333-3333-4333-8333-333333333333",
		State: StateUploading, ExpectedSize: 65, ExpectedChunks: 3, ExpiresAt: expires,
	}}}
	handler := NewHandler(service, testUserID)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/uploads/{id}", handler.Get)
	mux.HandleFunc("GET /api/v1/uploads/{id}/chunks", handler.Chunks)
	for _, path := range []string{"/api/v1/uploads/22222222-2222-4222-8222-222222222222", "/api/v1/uploads/22222222-2222-4222-8222-222222222222/chunks"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d", path, response.Code)
		}
		if !strings.Contains(response.Body.String(), `"completedIndexes":[]`) {
			t.Errorf("%s body = %s", path, response.Body.String())
		}
	}
}

func TestHandlerMapsErrorsAndMissingConfiguration(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{domain.ErrInvalid, http.StatusBadRequest}, {domain.ErrNotFound, http.StatusNotFound},
		{domain.ErrConflict, http.StatusConflict}, {domain.ErrInvalidState, http.StatusConflict},
		{placement.ErrInsufficientStorage, http.StatusInsufficientStorage},
		{storage.ErrQuotaExceeded, http.StatusInsufficientStorage},
		{storage.ErrUnavailable, http.StatusServiceUnavailable},
		{errors.New("database"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		handler := NewHandler(&stubUploadService{getErr: test.err}, testUserID)
		request := httptest.NewRequest(http.MethodGet, "/api/v1/uploads/id", nil)
		request.SetPathValue("id", "id")
		response := httptest.NewRecorder()
		handler.Get(response, request)
		if response.Code != test.want {
			t.Errorf("error %v status = %d, want %d", test.err, response.Code, test.want)
		}
	}
	handler := NewHandler(&stubUploadService{}, "")
	response := httptest.NewRecorder()
	handler.Create(response, httptest.NewRequest(http.MethodPost, "/api/v1/uploads", strings.NewReader(`{}`)))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing user status = %d", response.Code)
	}
}

func TestHandlerUploadChunk(t *testing.T) {
	service := &stubUploadService{chunkResult: ChunkResult{
		Index: 2, State: chunk.StateStored, SizeBytes: 1, ChecksumSHA256: strings.Repeat("a", 64),
	}}
	handler := NewHandler(service, testUserID)
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/v1/uploads/{id}/chunks/{index}", handler.UploadChunk)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/uploads/22222222-2222-4222-8222-222222222222/chunks/2", strings.NewReader("x")))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.chunkIndex != 2 || service.chunkUploadID != "22222222-2222-4222-8222-222222222222" || service.chunkContentLength != 1 {
		t.Fatalf("chunk service call = upload %q index %d length %d", service.chunkUploadID, service.chunkIndex, service.chunkContentLength)
	}
	var body chunkHTTPResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.State != chunk.StateStored || body.Checksum != service.chunkResult.ChecksumSHA256 {
		t.Fatalf("response = %+v", body)
	}

	response = httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/v1/uploads/22222222-2222-4222-8222-222222222222/chunks/not-an-index", strings.NewReader("x")))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid index status = %d", response.Code)
	}
}

func TestHandlerComplete(t *testing.T) {
	service := &stubUploadService{completionResult: CompletionResult{
		FileID: "33333333-3333-4333-8333-333333333333", State: filedomain.StateAvailable,
		ChecksumSHA256: strings.Repeat("a", 64),
	}}
	handler := NewHandler(service, testUserID)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/uploads/22222222-2222-4222-8222-222222222222/complete", nil)
	request.SetPathValue("id", "22222222-2222-4222-8222-222222222222")
	response := httptest.NewRecorder()
	handler.Complete(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.completeUploadID != "22222222-2222-4222-8222-222222222222" ||
		!strings.Contains(response.Body.String(), `"state":"AVAILABLE"`) ||
		!strings.Contains(response.Body.String(), service.completionResult.ChecksumSHA256) {
		t.Fatalf("completion call/response = %q / %s", service.completeUploadID, response.Body.String())
	}
}

type stubUploadService struct {
	created            CreatedUpload
	status             Status
	createErr          error
	getErr             error
	createUserID       string
	createRequest      CreateRequest
	chunkResult        ChunkResult
	chunkErr           error
	chunkUploadID      string
	chunkIndex         int
	chunkContentLength int64
	completionResult   CompletionResult
	completionErr      error
	completeUploadID   string
}

func (s *stubUploadService) Create(_ context.Context, userID string, request CreateRequest) (CreatedUpload, error) {
	s.createUserID, s.createRequest = userID, request
	return s.created, s.createErr
}
func (s *stubUploadService) Get(context.Context, string, string) (Status, error) {
	return s.status, s.getErr
}
func (s *stubUploadService) UploadChunk(_ context.Context, _ string, uploadID string, index int, _ io.Reader, contentLength int64) (ChunkResult, error) {
	s.chunkUploadID, s.chunkIndex, s.chunkContentLength = uploadID, index, contentLength
	return s.chunkResult, s.chunkErr
}
func (s *stubUploadService) Complete(_ context.Context, _ string, uploadID string) (CompletionResult, error) {
	s.completeUploadID = uploadID
	return s.completionResult, s.completionErr
}
