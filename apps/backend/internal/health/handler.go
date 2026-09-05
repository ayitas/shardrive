package health

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type Pinger interface {
	Ping(context.Context) error
}

type Handler struct {
	database Pinger
	timeout  time.Duration
}

func NewHandler(database Pinger, timeout time.Duration) *Handler {
	return &Handler{database: database, timeout: timeout}
}

func (h *Handler) Live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, "ok")
}

func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	if err := h.database.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	writeJSON(w, http.StatusOK, "ok")
}

func writeJSON(w http.ResponseWriter, status int, state string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": state})
}
