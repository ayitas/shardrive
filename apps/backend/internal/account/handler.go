package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
)

type userAccountRepository interface {
	Create(context.Context, CreateParams) (Account, error)
	ListByUser(context.Context, string) ([]Account, error)
	Refresh(context.Context, string, string, RefreshParams) (Account, error)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if h.repository == nil || err != nil {
		writeAccountError(w, http.StatusServiceUnavailable, "not_configured")
		return
	}
	var request struct {
		Name          string `json:"name"`
		Provider      string `json:"provider"`
		CredentialRef string `json:"credentialRef"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || strings.TrimSpace(request.Name) == "" || request.Provider != "proton" || !validCredentialRef(request.CredentialRef) {
		writeAccountError(w, http.StatusBadRequest, "invalid_account")
		return
	}
	value, err := h.repository.Create(r.Context(), CreateParams{
		UserID: userID, Name: strings.TrimSpace(request.Name), Provider: request.Provider,
		TotalBytes: 0, MaxUploadWorkers: 2, MaxDownloadWorkers: 1,
		CredentialRef: &request.CredentialRef,
	})
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			writeAccountError(w, http.StatusConflict, "account_exists")
			return
		}
		writeAccountError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	healthy := false
	if h.refresher != nil {
		refreshed, refreshErr := h.refresh(r.Context(), userID, value.ID)
		if refreshErr != nil {
			writeAccountError(w, http.StatusInternalServerError, "refresh_failed")
			return
		}
		value, healthy = refreshed.account, refreshed.healthy
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"account": accountSummaryJSON(value), "healthy": healthy})
}

type AccountRefresher interface {
	Refresh(context.Context, Account) (RefreshObservation, error)
}
type Handler struct {
	repository userAccountRepository
	userID     string
	refresher  AccountRefresher
}

func NewHandler(repository userAccountRepository, userID string, refreshers ...AccountRefresher) *Handler {
	var refresher AccountRefresher
	if len(refreshers) > 0 {
		refresher = refreshers[0]
	}
	return &Handler{repository: repository, userID: userID, refresher: refresher}
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

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	userID, err := domain.NormalizeUUID(h.requestUserID(r))
	if h.repository == nil || h.refresher == nil || err != nil {
		writeAccountError(w, http.StatusServiceUnavailable, "not_configured")
		return
	}
	accountID := r.PathValue("id")
	value, err := h.refresh(r.Context(), userID, accountID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeAccountError(w, http.StatusNotFound, "not_found")
			return
		}
		writeAccountError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeAccountSummary(w, value.account, value.healthy, value.errorCode)
}

type refreshResult struct {
	account   Account
	healthy   bool
	errorCode *string
}

func (h *Handler) refresh(ctx context.Context, userID, accountID string) (refreshResult, error) {
	values, err := h.repository.ListByUser(ctx, userID)
	if err != nil {
		return refreshResult{}, err
	}
	var current Account
	for _, candidate := range values {
		if candidate.ID == accountID {
			current = candidate
			break
		}
	}
	if current.ID == "" {
		return refreshResult{}, &domain.Error{Kind: domain.ErrNotFound, Op: "refresh", Entity: "storage account"}
	}
	observation, err := h.refresher.Refresh(ctx, current)
	if err != nil {
		// A provider outage must not leave the last state looking healthy. The
		// wiring layer normally converts provider errors into observations, but
		// keep this boundary safe for unexpected adapter/refresher failures too.
		code := "refresh_failed"
		return h.persistRefresh(ctx, userID, current, RefreshParams{
			State: StateOffline, TotalBytes: current.TotalBytes,
			UsedBytes: current.UsedBytes, FreeBytes: current.FreeBytes,
			ErrorCode: &code,
		}, false, code)
	}
	if observation.TotalBytes < 0 || observation.UsedBytes < 0 || observation.FreeBytes < 0 || observation.UsedBytes > observation.TotalBytes || observation.FreeBytes > observation.TotalBytes {
		code := "invalid_usage"
		return h.persistRefresh(ctx, userID, current, RefreshParams{State: StateDegraded, TotalBytes: current.TotalBytes, UsedBytes: current.UsedBytes, FreeBytes: current.FreeBytes, ErrorCode: &code}, false, code)
	}
	if observation.State == "" {
		observation.State = StateActive
		if observation.FreeBytes == 0 {
			observation.State = StateFull
		}
	}
	var errorCode *string
	if observation.ErrorCode != "" {
		errorCode = &observation.ErrorCode
	}
	return h.persistRefresh(ctx, userID, current, RefreshParams{State: observation.State, TotalBytes: observation.TotalBytes, UsedBytes: observation.UsedBytes, FreeBytes: observation.FreeBytes, ErrorCode: errorCode}, observation.Healthy, observation.ErrorCode)
}

func (h *Handler) persistRefresh(ctx context.Context, userID string, current Account, params RefreshParams, healthy bool, errorCode string) (refreshResult, error) {
	value, err := h.repository.Refresh(ctx, userID, current.ID, params)
	if err != nil {
		return refreshResult{}, err
	}
	var code *string
	if errorCode != "" {
		code = &errorCode
	}
	return refreshResult{account: value, healthy: healthy, errorCode: code}, nil
}

func writeAccountSummary(w http.ResponseWriter, value Account, healthy bool, errorCode *string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"account": accountSummaryJSON(value), "healthy": healthy, "errorCode": errorCode})
}

type accountSummary struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	State      State  `json:"state"`
	TotalBytes int64  `json:"totalBytes"`
	UsedBytes  int64  `json:"usedBytes"`
	FreeBytes  int64  `json:"freeBytes"`
}

func accountSummaryJSON(value Account) accountSummary {
	return accountSummary{value.ID, value.Name, value.Provider, value.State, value.TotalBytes, value.UsedBytes, value.FreeBytes}
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

func validCredentialRef(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') && (character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}
