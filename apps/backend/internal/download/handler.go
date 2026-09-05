package download

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

type downloadService interface {
	Prepare(context.Context, string, string) (Plan, error)
	Stream(context.Context, Plan, io.Writer) error
}

type Handler struct {
	service downloadService
	userID  string
}

func NewHandler(service downloadService, userID string) *Handler {
	return &Handler{service: service, userID: userID}
}

func (h *Handler) Download(w http.ResponseWriter, r *http.Request) {
	_, sessionUser := auth.UserID(r.Context())
	if h.service == nil || h.userID == "" && !sessionUser {
		writeError(w, http.StatusServiceUnavailable, "not_configured", "download API requires a configured user")
		return
	}
	userID := h.userID
	if value, ok := auth.UserID(r.Context()); ok {
		userID = value
	}
	plan, err := h.service.Prepare(r.Context(), userID, r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Content-Length", strconv.FormatInt(plan.File.SizeBytes, 10))
	w.Header().Set("Content-Type", safeContentType(plan.File.MIMEType))
	w.Header().Set("Content-Disposition", safeContentDisposition(plan.File.Name))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_ = h.service.Stream(r.Context(), plan, w)
}

func safeContentType(value string) string {
	mediaType, parameters, err := mime.ParseMediaType(value)
	if err != nil {
		return "application/octet-stream"
	}
	formatted := mime.FormatMediaType(mediaType, parameters)
	if formatted == "" {
		return "application/octet-stream"
	}
	return formatted
}

func safeContentDisposition(filename string) string {
	value := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if value == "" {
		return "attachment"
	}
	return value
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", "request is invalid")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "file was not found")
	case errors.Is(err, domain.ErrInvalidState):
		writeError(w, http.StatusConflict, "file_unavailable", "file is not available")
	case errors.Is(err, ErrIntegrity):
		writeError(w, http.StatusConflict, "integrity_failure", "file storage metadata or objects are inconsistent")
	case errors.Is(err, storage.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "storage provider is unavailable")
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": message}})
}
