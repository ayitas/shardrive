package repositorytest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/job"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	localstorage "github.com/ayitas/shardrive/apps/backend/internal/storage/local"
)

func TestPostgresCleanupWorkerDeletesRemoteChunk(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var userID, fileID string
	if err := pool.QueryRow(ctx, "INSERT INTO users (email, password_hash) VALUES ('worker-cleanup@example.test', 'unused') RETURNING id").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "INSERT INTO files (user_id, name, size_bytes, chunk_size, chunk_count, state) VALUES ($1, 'cleanup.bin', 3, 3, 1, 'DELETING') RETURNING id", userID).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	acc, err := account.NewRepository(pool).Create(ctx, account.CreateParams{UserID: userID, Name: "local-cleanup", Provider: "local", TotalBytes: 100, MaxUploadWorkers: 1, MaxDownloadWorkers: 1})
	if err != nil {
		t.Fatal(err)
	}
	provider, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	object, err := provider.Upload(ctx, acc, storage.UploadRequest{ObjectID: "00000000-0000-4000-8000-000000000001", SizeBytes: 3, Body: strings.NewReader("abc")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO chunks (file_id, chunk_index, size_bytes, checksum_sha256, storage_account_id, remote_object_id, state) VALUES ($1, 0, 3, repeat('a', 64), $2, $3, 'STORED')", fileID, acc.ID, object.ObjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE storage_accounts SET used_bytes = 3 WHERE id = $1", acc.ID); err != nil {
		t.Fatal(err)
	}
	jobs := job.NewRepository(pool)
	if _, err := jobs.Enqueue(ctx, userID, job.CleanupUpload, map[string]string{"fileId": fileID}, 3); err != nil {
		t.Fatal(err)
	}
	registry := storage.NewRegistry()
	if err := registry.Register("local", provider); err != nil {
		t.Fatal(err)
	}
	worker := job.NewCleanupWorker(jobs, chunk.NewRepository(pool), account.NewRepository(pool), registry, "integration-worker", filedomain.NewRepository(pool))
	if err := worker.ProcessOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Stat(ctx, acc, object.ObjectID); err == nil {
		t.Fatal("remote object still exists")
	}
	var state, fileState string
	var used int64
	if err := pool.QueryRow(ctx, "SELECT c.state, a.used_bytes, f.state FROM chunks c JOIN storage_accounts a ON a.id = c.storage_account_id JOIN files f ON f.id = c.file_id WHERE c.file_id = $1", fileID).Scan(&state, &used, &fileState); err != nil {
		t.Fatal(err)
	}
	if state != "DELETED" || used != 0 || fileState != "DELETED" {
		t.Fatalf("state/usage/file-state = %s/%d/%s", state, used, fileState)
	}
}
