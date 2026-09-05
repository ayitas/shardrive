package repositorytest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/health"
	"github.com/ayitas/shardrive/apps/backend/internal/httpapi"
	"github.com/ayitas/shardrive/apps/backend/internal/job"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	localstorage "github.com/ayitas/shardrive/apps/backend/internal/storage/local"
	"github.com/ayitas/shardrive/apps/backend/internal/upload"
)

func TestPostgresUploadCancellationEnqueuesCleanup(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var userID string
	if err := pool.QueryRow(ctx, "INSERT INTO users (email, password_hash) VALUES ('cancel-upload@example.test', 'unused') RETURNING id").Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err := account.NewRepository(pool).ProvisionLocal(ctx, account.ProvisionLocalParams{UserID: userID, Count: 1, TotalBytes: 100, MaxUploadWorkers: 1, MaxDownloadWorkers: 1}); err != nil {
		t.Fatal(err)
	}
	provider, err := localstorage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := storage.NewRegistry()
	if err := registry.Register("local", provider); err != nil {
		t.Fatal(err)
	}
	placementConfig := placement.DefaultConfig()
	placementConfig.MinimumReserveBytes = 0
	placementConfig.ReserveChunkMultiplier = 0
	engine, err := placement.New(placementConfig)
	if err != nil {
		t.Fatal(err)
	}
	files := filedomain.NewRepository(pool)
	service, err := upload.NewService(upload.NewRepository(pool), chunk.NewRepository(pool), account.NewRepository(pool), registry, engine, upload.ServiceConfig{ChunkSize: 32, SessionLifetime: time.Hour, GlobalUploadLimit: 1}, files)
	if err != nil {
		t.Fatal(err)
	}
	jobs := job.NewRepository(pool)
	handler := upload.NewHandler(service, userID, jobs)
	server := httptest.NewServer(httpapi.NewRouter(health.NewHandler(pool, time.Second), handler, nil))
	defer server.Close()

	create, err := server.Client().Post(server.URL+"/api/v1/uploads", "application/json", strings.NewReader(`{"name":"cancel.bin","size":3}`))
	if err != nil {
		t.Fatal(err)
	}
	if create.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(create.Body)
		create.Body.Close()
		t.Fatalf("create status=%d body=%s", create.StatusCode, body)
	}
	var created struct {
		UploadID string `json:"uploadId"`
		FileID   string `json:"fileId"`
	}
	if err := json.NewDecoder(create.Body).Decode(&created); err != nil {
		create.Body.Close()
		t.Fatal(err)
	}
	create.Body.Close()

	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, server.URL+"/api/v1/uploads/"+created.UploadID, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("cancel status=%d", response.StatusCode)
	}
	var sessionState, fileState, jobState, payload string
	if err := pool.QueryRow(ctx, `SELECT s.state, f.state, j.state, j.payload::text
		FROM upload_sessions s JOIN files f ON f.id = s.file_id
		JOIN jobs j ON j.user_id = s.user_id
		WHERE s.id = $1 AND j.type = 'CLEANUP_UPLOAD'`, created.UploadID).Scan(&sessionState, &fileState, &jobState, &payload); err != nil {
		t.Fatal(err)
	}
	if sessionState != "CANCELLED" || fileState != "DELETING" || jobState != "PENDING" {
		t.Fatalf("states=%s/%s/%s", sessionState, fileState, jobState)
	}
	if !strings.Contains(payload, created.FileID) {
		t.Fatalf("cleanup payload %q does not contain file id %s", payload, created.FileID)
	}
	second, err := server.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	second.Body.Close()
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("repeated cancel status=%d", second.StatusCode)
	}
	var jobCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM jobs WHERE user_id = $1 AND type = 'CLEANUP_UPLOAD'", userID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	if jobCount != 1 {
		t.Fatalf("cleanup job count=%d, want 1", jobCount)
	}
}
