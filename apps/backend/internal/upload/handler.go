package upload

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/job"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

const maxCreateRequestBytes = 64 * 1024

type uploadService interface {
	Create(context.Context, string, CreateRequest) (CreatedUpload, error)
	Get(context.Context, string, string) (Status, error)
	UploadChunk(context.Context, string, string, int, io.Reader, int64) (ChunkResult, error)
	Complete(context.Context, string, string) (CompletionResult, error)
}
type cancellableUpload interface {
	Cancel(context.Context, string, string) error
}

type chunkHTTPResponse struct {
	Index    int         `json:"index"`
	State    chunk.State `json:"state"`
	Bytes    int64       `json:"bytes"`
	Checksum string      `json:"checksum"`
}

type Handler struct {
	service uploadService
	userID  string
	jobs    interface {
		Enqueue(context.Context, string, job.Type, any, int) (string, error)
	}
}

func NewHandler(service uploadService, userID string, jobs ...interface {
	Enqueue(context.Context, string, job.Type, any, int) (string, error)
}) *Handler {
	var repository interface {
		Enqueue(context.Context, string, job.Type, any, int) (string, error)
	}
	if len(jobs) > 0 {
		repository = jobs[0]
	}
	return &Handler{service: service, userID: userID, jobs: repository}
}

type createHTTPRequest struct {
	Name        string  `json:"name"`
	Size        int64   `json:"size"`
	MIMEType    string  `json:"mimeType"`
	DirectoryID *string `json:"directoryId"`
}

type createHTTPResponse struct {
	UploadID   string `json:"uploadId"`
	FileID     string `json:"fileId"`
	ChunkSize  int64  `json:"chunkSize"`
	ChunkCount int    `json:"chunkCount"`
}

type statusHTTPResponse struct {
	UploadID         string    `json:"uploadId"`
	FileID           string    `json:"fileId"`
	State            State     `json:"state"`
	ExpectedSize     int64     `json:"expectedSize"`
	ReceivedBytes    int64     `json:"receivedBytes"`
	ExpectedChunks   int       `json:"expectedChunks"`
	CompletedChunks  int       `json:"completedChunks"`
	CompletedIndexes []int     `json:"completedIndexes"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.requireUser(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCreateRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request createHTTPRequest
	if err := decoder.Decode(&request); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	if err := ensureJSONEnd(decoder); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return
	}
	created, err := h.service.Create(r.Context(), h.requestUserID(r), CreateRequest{
		Name: request.Name, SizeBytes: request.Size, MIMEType: request.MIMEType, DirectoryID: request.DirectoryID,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusCreated, createHTTPResponse{
		UploadID: created.Session.ID, FileID: created.File.ID,
		ChunkSize: created.File.ChunkSize, ChunkCount: created.File.ChunkCount,
	})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !h.requireUser(w, r) {
		return
	}
	status, err := h.service.Get(r.Context(), h.requestUserID(r), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, responseFromStatus(status))
}

func (h *Handler) Chunks(w http.ResponseWriter, r *http.Request) {
	if !h.requireUser(w, r) {
		return
	}
	status, err := h.service.Get(r.Context(), h.requestUserID(r), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	completedIndexes := status.CompletedIndexes
	if completedIndexes == nil {
		completedIndexes = []int{}
	}
	writeAPIJSON(w, http.StatusOK, struct {
		UploadID         string `json:"uploadId"`
		CompletedIndexes []int  `json:"completedIndexes"`
	}{UploadID: status.Session.ID, CompletedIndexes: completedIndexes})
}

func (h *Handler) UploadChunk(w http.ResponseWriter, r *http.Request) {
	if !h.requireUser(w, r) {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "chunk index must be a non-negative integer")
		return
	}
	result, err := h.service.UploadChunk(r.Context(), h.requestUserID(r), r.PathValue("id"), index, r.Body, r.ContentLength)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, chunkHTTPResponse{
		Index: result.Index, State: result.State, Bytes: result.SizeBytes, Checksum: result.ChecksumSHA256,
	})
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	if !h.requireUser(w, r) {
		return
	}
	result, err := h.service.Complete(r.Context(), h.requestUserID(r), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeAPIJSON(w, http.StatusOK, struct {
		FileID   string           `json:"fileId"`
		State    filedomain.State `json:"state"`
		Checksum string           `json:"checksum"`
	}{FileID: result.FileID, State: result.State, Checksum: result.ChecksumSHA256})
}

func (h *Handler) Cancel(w http.ResponseWriter, r *http.Request) {
	if !h.requireUser(w, r) || h.jobs == nil {
		writeAPIError(w, http.StatusServiceUnavailable, "not_configured", "upload cleanup is not configured")
		return
	}
	cancellable, ok := h.service.(cancellableUpload)
	if !ok {
		writeAPIError(w, http.StatusServiceUnavailable, "not_configured", "upload cancellation is not configured")
		return
	}
	status, err := h.service.Get(r.Context(), h.requestUserID(r), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if err := cancellable.Cancel(r.Context(), h.requestUserID(r), r.PathValue("id")); err != nil {
		writeServiceError(w, err)
		return
	}
	if _, err := h.jobs.Enqueue(r.Context(), h.requestUserID(r), job.CleanupUpload, map[string]string{"fileId": status.Session.FileID}, 5); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "could not enqueue upload cleanup")
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) requestUserID(r *http.Request) string {
	if userID, ok := auth.UserID(r.Context()); ok {
		return userID
	}
	return h.userID
}

func (h *Handler) requireUser(w http.ResponseWriter, r *http.Request) bool {
	if h.service == nil || h.requestUserID(r) == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "not_configured", "upload API requires a configured user")
		return false
	}
	return true
}

func responseFromStatus(status Status) statusHTTPResponse {
	completedIndexes := status.CompletedIndexes
	if completedIndexes == nil {
		completedIndexes = []int{}
	}
	return statusHTTPResponse{
		UploadID: status.Session.ID, FileID: status.Session.FileID, State: status.Session.State,
		ExpectedSize: status.Session.ExpectedSize, ReceivedBytes: status.Session.ReceivedBytes,
		ExpectedChunks: status.Session.ExpectedChunks, CompletedChunks: status.Session.CompletedChunks,
		CompletedIndexes: completedIndexes, ExpiresAt: status.Session.ExpiresAt,
	}
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("extra JSON value")
	}
	return err
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalid):
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, domain.ErrNotFound):
		writeAPIError(w, http.StatusNotFound, "not_found", "upload was not found")
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrInvalidState):
		writeAPIError(w, http.StatusConflict, "conflict", "upload conflicts with existing state")
	case errors.Is(err, placement.ErrInsufficientStorage), errors.Is(err, storage.ErrQuotaExceeded):
		writeAPIError(w, http.StatusInsufficientStorage, "insufficient_storage", "no storage account can accept this chunk")
	case errors.Is(err, storage.ErrUnavailable):
		writeAPIError(w, http.StatusServiceUnavailable, "storage_unavailable", "storage provider is unavailable")
	default:
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeAPIJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeAPIJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
