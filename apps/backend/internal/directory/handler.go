package directory

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

type repository interface {
	ListByUser(context.Context, string, *string) ([]Directory, error)
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
