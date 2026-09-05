package repositorytest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/job"
	"github.com/jackc/pgx/v5"
)

func TestPostgresJobEnqueueClaimComplete(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var userID string
	if err := pool.QueryRow(ctx, "INSERT INTO users (email, password_hash) VALUES ('job@example.test', 'unused') RETURNING id").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	repo := job.NewRepository(pool)
	id, err := repo.Enqueue(ctx, userID, job.CleanupUpload, map[string]string{"fileId": "file-1"}, 3)
	if err != nil || id == "" {
		t.Fatalf("enqueue id/error: %q %v", id, err)
	}
	claimed, err := repo.Claim(ctx, "worker-test")
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != id || claimed.State != job.Running || claimed.Attempts != 1 || claimed.UserID != userID {
		t.Fatalf("claimed = %+v", claimed)
	}
	if err := repo.Complete(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Claim(ctx, "worker-test-2"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claim completed error = %v", err)
	}
}
