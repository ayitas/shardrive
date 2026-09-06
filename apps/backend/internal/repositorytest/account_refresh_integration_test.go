package repositorytest

import (
	"context"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
)

func TestPostgresAccountRefreshPersistsHealthAndQuota(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ('account-refresh@example.test', 'not-a-real-password-hash')
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	repository := account.NewRepository(pool)
	value, err := repository.Create(ctx, account.CreateParams{
		UserID: userID, Name: "local-refresh", Provider: "local", TotalBytes: 100,
		MaxUploadWorkers: 1, MaxDownloadWorkers: 1,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	errorCode := "provider_unavailable"
	refreshed, err := repository.Refresh(ctx, userID, value.ID, account.RefreshParams{
		State: account.StateOffline, TotalBytes: 100, UsedBytes: 25, FreeBytes: 75, ErrorCode: &errorCode,
	})
	if err != nil {
		t.Fatalf("refresh account: %v", err)
	}
	if refreshed.State != account.StateOffline || refreshed.UsedBytes != 25 || refreshed.FreeBytes != 75 {
		t.Fatalf("refreshed account = %+v", refreshed)
	}
	if refreshed.LastHealthCheck == nil || refreshed.LastError == nil || *refreshed.LastError != errorCode {
		t.Fatalf("refresh metadata = health=%v error=%v", refreshed.LastHealthCheck, refreshed.LastError)
	}
	if _, err := repository.Refresh(ctx, "00000000-0000-4000-8000-000000000002", value.ID, account.RefreshParams{State: account.StateActive, TotalBytes: 100, UsedBytes: 0, FreeBytes: 100}); err == nil {
		t.Fatal("refresh for another user succeeded")
	}
}
