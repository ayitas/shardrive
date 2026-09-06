package account

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllAccountStatesAreValid(t *testing.T) {
	states := []State{
		StateActive, StateDegraded, StateFull, StateOffline, StateRateLimited,
		StateAuthFailed, StateDisabled, StateAuthRequired,
	}
	for _, state := range states {
		if !state.Valid() {
			t.Errorf("%q.Valid() = false", state)
		}
	}
	if State("UNKNOWN").Valid() {
		t.Fatal("UNKNOWN.Valid() = true")
	}
}

type refreshRepositoryStub struct {
	accounts          []Account
	refreshed         RefreshParams
	userID, accountID string
}

func (s *refreshRepositoryStub) ListByUser(context.Context, string) ([]Account, error) {
	return s.accounts, nil
}
func (s *refreshRepositoryStub) Refresh(_ context.Context, userID, accountID string, params RefreshParams) (Account, error) {
	s.userID, s.accountID, s.refreshed = userID, accountID, params
	value := s.accounts[0]
	value.State, value.TotalBytes, value.UsedBytes, value.FreeBytes = params.State, params.TotalBytes, params.UsedBytes, params.FreeBytes
	return value, nil
}

type refreshProviderStub struct{ observation RefreshObservation }

func (s refreshProviderStub) Refresh(context.Context, Account) (RefreshObservation, error) {
	return s.observation, nil
}

func TestRefreshPersistsProviderNeutralObservation(t *testing.T) {
	const userID = "00000000-0000-4000-8000-000000000001"
	repository := &refreshRepositoryStub{accounts: []Account{{ID: "account-1", UserID: userID, Name: "local-1", Provider: "local"}}}
	handler := NewHandler(repository, userID, refreshProviderStub{observation: RefreshObservation{TotalBytes: 100, UsedBytes: 40, FreeBytes: 60, Healthy: true}})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/account-1/refresh", nil)
	request.SetPathValue("id", "account-1")
	response := httptest.NewRecorder()
	handler.Refresh(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if repository.userID != userID || repository.accountID != "account-1" {
		t.Fatalf("refresh ownership = %q/%q", repository.userID, repository.accountID)
	}
	if repository.refreshed.State != StateActive || repository.refreshed.UsedBytes != 40 {
		t.Fatalf("persisted refresh = %+v", repository.refreshed)
	}
}
