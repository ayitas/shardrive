package main

import (
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

func TestFailedObservationPreservesAuthRequiredState(t *testing.T) {
	observation := failedObservation(account.Account{}, storage.ErrAuthenticationRequired)
	if observation.State != account.StateAuthRequired || observation.ErrorCode != "authentication_required" {
		t.Fatalf("observation = %+v", observation)
	}
}
