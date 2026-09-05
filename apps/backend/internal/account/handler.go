package account

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

type userAccountRepository interface {
	ListByUser(context.Context, string) ([]Account, error)
}
type Handler struct {
	repository userAccountRepository
	userID     string
}

func NewHandler(repository userAccountRepository, userID string) *Handler {
	return &Handler{repository: repository, userID: userID}
}
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if h.repository == nil || err != nil {
		writeAccountError(w, http.StatusServiceUnavailable, "not_configured")
		return
	}
	values, err := h.repository.ListByUser(r.Context(), userID)
	if err != nil {
		writeAccountError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	type summary struct {
		ID         string `json:"id"`
		Name       string `json:"name"`
		Provider   string `json:"provider"`
		State      State  `json:"state"`
		TotalBytes int64  `json:"totalBytes"`
		UsedBytes  int64  `json:"usedBytes"`
		FreeBytes  int64  `json:"freeBytes"`
	}
	result := make([]summary, 0, len(values))
	for _, value := range values {
		result = append(result, summary{value.ID, value.Name, value.Provider, value.State, value.TotalBytes, value.UsedBytes, value.FreeBytes})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"accounts": result})
}
func (h *Handler) requestUserID(r *http.Request) string {
	if userID, ok := auth.UserID(r.Context()); ok {
		return userID
	}
	return h.userID
}
func writeAccountError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code}})
}
