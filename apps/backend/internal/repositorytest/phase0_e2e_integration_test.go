package repositorytest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/download"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/health"
	"github.com/ayitas/shardrive/apps/backend/internal/httpapi"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	localstorage "github.com/ayitas/shardrive/apps/backend/internal/storage/local"
	"github.com/ayitas/shardrive/apps/backend/internal/upload"
)

func TestPhase0HTTPRoundTripAfterApplicationRecreation(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ('phase0-e2e@example.test', 'not-a-real-password-hash') RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("create e2e user: %v", err)
	}
	accounts := account.NewRepository(pool)
	if _, err := accounts.ProvisionLocal(ctx, account.ProvisionLocalParams{
		UserID: userID, Count: 5, TotalBytes: 100, MaxUploadWorkers: 2, MaxDownloadWorkers: 1,
	}); err != nil {
		t.Fatalf("provision e2e accounts: %v", err)
	}
	localRoot := t.TempDir()
	provider, err := localstorage.New(localRoot)
	if err != nil {
		t.Fatalf("create e2e provider: %v", err)
	}
	registry := storage.NewRegistry()
	if err := registry.Register("local", provider); err != nil {
		t.Fatalf("register e2e provider: %v", err)
	}
	placementConfig := placement.DefaultConfig()
	placementConfig.MinimumReserveBytes = 0
	placementConfig.ReserveChunkMultiplier = 0
	placementEngine, err := placement.New(placementConfig)
	if err != nil {
		t.Fatalf("create e2e placement: %v", err)
	}
	newHandlers := func(registry *storage.Registry) (*upload.Handler, *download.Handler) {
		uploadService, serviceErr := upload.NewService(
			upload.NewRepository(pool), chunk.NewRepository(pool), account.NewRepository(pool), registry,
			placementEngine, upload.ServiceConfig{ChunkSize: 32, SessionLifetime: time.Hour, GlobalUploadLimit: 2},
		)
		if serviceErr != nil {
			t.Fatalf("create e2e upload service: %v", serviceErr)
		}
		downloadService, serviceErr := download.NewService(
			filedomain.NewRepository(pool), chunk.NewRepository(pool), account.NewRepository(pool), registry,
		)
		if serviceErr != nil {
			t.Fatalf("create e2e download service: %v", serviceErr)
		}
		return upload.NewHandler(uploadService, userID), download.NewHandler(downloadService, userID)
	}
	uploadHandler, downloadHandler := newHandlers(registry)
	server := httptest.NewServer(httpapi.NewRouter(health.NewHandler(pool, time.Second), uploadHandler, downloadHandler))

	payload := strings.Repeat("a", 32) + strings.Repeat("b", 32) + "c"
	originalChecksum := sha256.Sum256([]byte(payload))
	createBody := bytes.NewBufferString(`{"name":"phase0.bin","size":65}`)
	createResponse, err := server.Client().Post(server.URL+"/api/v1/uploads", "application/json", createBody)
	if err != nil {
		server.Close()
		t.Fatalf("HTTP create upload: %v", err)
	}
	if createResponse.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResponse.Body)
		createResponse.Body.Close()
		server.Close()
		t.Fatalf("HTTP create status = %d, body=%s", createResponse.StatusCode, body)
	}
	var created struct {
		UploadID string `json:"uploadId"`
		FileID   string `json:"fileId"`
	}
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		createResponse.Body.Close()
		server.Close()
		t.Fatalf("decode create response: %v", err)
	}
	createResponse.Body.Close()
	if created.UploadID == "" || created.FileID == "" {
		server.Close()
		t.Fatal("create response omitted upload/file IDs")
	}
	for index, body := range []string{strings.Repeat("a", 32), strings.Repeat("b", 32), "c"} {
		request, err := http.NewRequestWithContext(ctx, http.MethodPut,
			fmt.Sprintf("%s/api/v1/uploads/%s/chunks/%d", server.URL, created.UploadID, index), strings.NewReader(body))
		if err != nil {
			server.Close()
			t.Fatalf("create chunk request: %v", err)
		}
		request.ContentLength = int64(len(body))
		response, err := server.Client().Do(request)
		if err != nil {
			server.Close()
			t.Fatalf("HTTP upload chunk %d: %v", index, err)
		}
		if response.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(response.Body)
			response.Body.Close()
			server.Close()
			t.Fatalf("HTTP upload chunk %d status = %d, body=%s", index, response.StatusCode, body)
		}
		response.Body.Close()
	}
	completeResponse, err := server.Client().Post(server.URL+"/api/v1/uploads/"+created.UploadID+"/complete", "", nil)
	if err != nil {
		server.Close()
		t.Fatalf("HTTP complete upload: %v", err)
	}
	if completeResponse.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(completeResponse.Body)
		completeResponse.Body.Close()
		server.Close()
		t.Fatalf("HTTP complete status = %d, body=%s", completeResponse.StatusCode, body)
	}
	completeResponse.Body.Close()

	downloaded := getHTTPFile(t, server.Client(), server.URL, created.FileID)
	if downloaded != payload || sha256.Sum256([]byte(downloaded)) != originalChecksum {
		server.Close()
		t.Fatalf("HTTP downloaded payload/checksum differs before recreation")
	}
	downloadedChecksum := sha256.Sum256([]byte(downloaded))
	t.Logf("ORIGINAL_SHA256=%s DOWNLOADED_SHA256=%s", hex.EncodeToString(originalChecksum[:]), hex.EncodeToString(downloadedChecksum[:]))
	server.Close()

	recreatedProvider, err := localstorage.New(localRoot)
	if err != nil {
		t.Fatalf("recreate e2e provider: %v", err)
	}
	recreatedRegistry := storage.NewRegistry()
	if err := recreatedRegistry.Register("local", recreatedProvider); err != nil {
		t.Fatalf("register recreated e2e provider: %v", err)
	}
	recreatedUploadHandler, recreatedDownloadHandler := newHandlers(recreatedRegistry)
	recreatedServer := httptest.NewServer(httpapi.NewRouter(health.NewHandler(pool, time.Second), recreatedUploadHandler, recreatedDownloadHandler))
	defer recreatedServer.Close()
	recreatedDownloaded := getHTTPFile(t, recreatedServer.Client(), recreatedServer.URL, created.FileID)
	if recreatedDownloaded != payload || sha256.Sum256([]byte(recreatedDownloaded)) != originalChecksum {
		t.Fatalf("HTTP downloaded payload/checksum differs after recreation")
	}
	recreatedChecksum := sha256.Sum256([]byte(recreatedDownloaded))
	t.Logf("RECREATED_ORIGINAL_SHA256=%s RECREATED_DOWNLOADED_SHA256=%s", hex.EncodeToString(originalChecksum[:]), hex.EncodeToString(recreatedChecksum[:]))
	var distinctAccounts int
	if err := pool.QueryRow(ctx, "SELECT count(DISTINCT storage_account_id) FROM chunks c JOIN files f ON f.id = c.file_id WHERE f.id = $1", created.FileID).Scan(&distinctAccounts); err != nil {
		t.Fatalf("count e2e physical accounts: %v", err)
	}
	if distinctAccounts < 2 {
		t.Fatalf("e2e chunks were not distributed: %d accounts", distinctAccounts)
	}
}

func getHTTPFile(t *testing.T, client *http.Client, baseURL, fileID string) string {
	t.Helper()
	response, err := client.Get(baseURL + "/api/v1/files/" + fileID + "/download")
	if err != nil {
		t.Fatalf("HTTP download: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("HTTP download status = %d, body=%s", response.StatusCode, body)
	}
	if response.ContentLength != 65 {
		t.Fatalf("HTTP download Content-Length = %d, want 65", response.ContentLength)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read HTTP download: %v", err)
	}
	return string(body)
}
