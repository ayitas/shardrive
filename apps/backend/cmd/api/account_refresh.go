package main

import (
	"context"
	"errors"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

// providerAccountRefresher is the only wiring layer that translates concrete
// provider errors into the provider-neutral account observation used by the
// HTTP product surface.
type providerAccountRefresher struct{ providers *storage.Registry }

func (r providerAccountRefresher) Refresh(ctx context.Context, value account.Account) (account.RefreshObservation, error) {
	provider, err := r.providers.Get(value.Provider)
	if err != nil {
		return account.RefreshObservation{State: account.StateOffline, TotalBytes: value.TotalBytes, UsedBytes: value.UsedBytes, FreeBytes: value.FreeBytes, ErrorCode: "provider_unavailable"}, nil
	}
	if err := provider.Health(ctx, value); err != nil {
		return failedObservation(value, err), nil
	}
	usage, err := provider.Usage(ctx, value)
	if err != nil {
		observation := failedObservation(value, err)
		return observation, nil
	}
	state := account.StateActive
	if usage.FreeBytes == 0 {
		state = account.StateFull
	}
	return account.RefreshObservation{State: state, TotalBytes: usage.TotalBytes, UsedBytes: usage.UsedBytes, FreeBytes: usage.FreeBytes, Healthy: true}, nil
}

func failedObservation(value account.Account, err error) account.RefreshObservation {
	state := account.StateOffline
	code := "provider_unavailable"
	switch {
	case errors.Is(err, storage.ErrAuthentication):
		state, code = account.StateAuthFailed, "authentication_failed"
	case errors.Is(err, storage.ErrRateLimited):
		state, code = account.StateRateLimited, "rate_limited"
	case errors.Is(err, storage.ErrQuotaExceeded):
		state, code = account.StateFull, "quota_exceeded"
	}
	return account.RefreshObservation{State: state, TotalBytes: value.TotalBytes, UsedBytes: value.UsedBytes, FreeBytes: value.FreeBytes, ErrorCode: code}
}
