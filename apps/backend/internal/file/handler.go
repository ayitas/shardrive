package file

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

type fileRepository interface {
	ListByUser(context.Context, string) ([]File, error)
}
type deletionRepository interface {
	GetForUser(context.Context, string, string) (File, error)
	Transition(context.Context, string, State, State) (File, error)
}

type Handler struct {
	repository fileRepository
	userID     string
	jobs       func(context.Context, string, string, any, int) (string, error)
}

func NewHandler(repository fileRepository, userID string, jobs ...func(context.Context, string, string, any, int) (string, error)) *Handler {
	var queue func(context.Context, string, string, any, int) (string, error)
	if len(jobs) > 0 {
		queue = jobs[0]
	}
	return &Handler{repository: repository, userID: userID, jobs: queue}
}

type listResponse struct {
	Files []map[string]any `json:"files"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if h.repository == nil || h.requestUserID(r) == "" {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "file API requires a configured user")
		return
	}
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "file API user is invalid")
		return
	}
	files, err := h.repository.ListByUser(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not list files")
		return
	}
	response := make([]map[string]any, 0, len(files))
	for _, file := range files {
		response = append(response, map[string]any{
			"id": file.ID, "directoryId": file.DirectoryID, "name": file.Name,
			"mimeType": file.MIMEType, "sizeBytes": file.SizeBytes, "state": file.State,
			"chunkCount": file.ChunkCount, "checksum": file.ChecksumSHA256,
			"createdAt": file.CreatedAt, "updatedAt": file.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, listResponse{Files: response})
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	deletable, ok := h.repository.(deletionRepository)
	if !ok || h.jobs == nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "file deletion is not configured")
		return
	}
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "file API user is invalid")
		return
	}
	value, err := deletable.GetForUser(r.Context(), r.PathValue("id"), userID)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "file was not found")
		return
	}
	if value.State != StateAvailable && value.State != StateDegraded {
		writeError(w, http.StatusConflict, "conflict", "file cannot be deleted in its current state")
		return
	}
	if _, err := deletable.Transition(r.Context(), value.ID, value.State, StateDeleting); err != nil {
		writeError(w, http.StatusConflict, "conflict", "file state changed")
		return
	}
	if _, err := h.jobs(r.Context(), userID, "DELETE_FILE", map[string]string{"fileId": value.ID}, 5); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not enqueue file cleanup")
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

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
