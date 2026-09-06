package directory

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

type repository interface {
	ListByUser(context.Context, string, *string) ([]Directory, error)
}
type createRepository interface {
	Create(context.Context, string, string, *string) (Directory, error)
}
type renameRepository interface {
	Rename(context.Context, string, string, string) (Directory, error)
}
type deleteRepository interface {
	DeleteEmpty(context.Context, string, string) error
}
type Handler struct {
	repository repository
	userID     string
}

func NewHandler(repository repository, userID string) *Handler {
	return &Handler{repository: repository, userID: userID}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	if h.repository == nil || h.requestUserID(r) == "" {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory API requires a configured user")
		return
	}
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory API user is invalid")
		return
	}
	var parentID *string
	if raw := r.URL.Query().Get("parentId"); raw != "" {
		normalized, normalizeErr := domain.NormalizeUUID(raw)
		if normalizeErr != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "parentId must be a UUID")
			return
		}
		parentID = &normalized
	}
	directories, err := h.repository.ListByUser(r.Context(), userID, parentID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "could not list directories")
		return
	}
	if directories == nil {
		directories = []Directory{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"directories": directories})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	creator, ok := h.repository.(createRepository)
	if !ok || h.requestUserID(r) == "" {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory creation is not configured")
		return
	}
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory API user is invalid")
		return
	}
	var request struct {
		Name     string  `json:"name"`
		ParentID *string `json:"parentId"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || len(name) > 255 {
		writeError(w, http.StatusBadRequest, "invalid_request", "name must be between 1 and 255 characters")
		return
	}
	var parentID *string
	if request.ParentID != nil && strings.TrimSpace(*request.ParentID) != "" {
		normalized, normalizeErr := domain.NormalizeUUID(strings.TrimSpace(*request.ParentID))
		if normalizeErr != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "parentId must be a UUID")
			return
		}
		parentID = &normalized
	}
	value, err := creator.Create(r.Context(), userID, name, parentID)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "a directory with that name already exists")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "parent directory was not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create directory")
		return
	}
	writeJSON(w, http.StatusCreated, value)
}

func (h *Handler) Rename(w http.ResponseWriter, r *http.Request) {
	renamer, ok := h.repository.(renameRepository)
	if !ok || h.requestUserID(r) == "" {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory rename is not configured")
		return
	}
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory API user is invalid")
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}
	name := strings.TrimSpace(request.Name)
	if name == "" || len(name) > 255 {
		writeError(w, http.StatusBadRequest, "invalid_request", "name must be between 1 and 255 characters")
		return
	}
	value, err := renamer.Rename(r.Context(), r.PathValue("id"), userID, name)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			writeError(w, http.StatusConflict, "conflict", "a directory with that name already exists")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "directory was not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not rename directory")
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	deleter, ok := h.repository.(deleteRepository)
	if !ok || h.requestUserID(r) == "" {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory deletion is not configured")
		return
	}
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "directory API user is invalid")
		return
	}
	if err := deleter.DeleteEmpty(r.Context(), r.PathValue("id"), userID); err != nil {
		if errors.Is(err, domain.ErrConflict) {
			writeError(w, http.StatusConflict, "not_empty", "folder must be empty before it can be deleted")
			return
		}
		if errors.Is(err, domain.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "directory was not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "could not delete directory")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
