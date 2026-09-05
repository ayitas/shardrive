package repositorytest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/db"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	"github.com/ayitas/shardrive/apps/backend/internal/download"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/health"
	"github.com/ayitas/shardrive/apps/backend/internal/httpapi"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	localstorage "github.com/ayitas/shardrive/apps/backend/internal/storage/local"
	"github.com/ayitas/shardrive/apps/backend/internal/upload"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresRepositoriesAndDuplicateChunkRace(t *testing.T) {
	pool := newTestDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash)
		VALUES ('repository-test@example.test', 'not-a-real-password-hash')
		RETURNING id
	`).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}

	accounts := account.NewRepository(pool)
	storedAccount, err := accounts.Create(ctx, account.CreateParams{
		UserID: userID, Name: "local-1", Provider: "local", TotalBytes: 1 << 30,
		Priority: 10, MaxUploadWorkers: 2, MaxDownloadWorkers: 1,
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if storedAccount.State != account.StateActive || storedAccount.FreeBytes != 1<<30 {
		t.Fatalf("created account = %+v", storedAccount)
	}
	listedAccounts, err := accounts.ListByUser(ctx, userID)
	if err != nil || len(listedAccounts) != 1 {
		t.Fatalf("list accounts = %+v, %v", listedAccounts, err)
	}
	provisioned, err := accounts.ProvisionLocal(ctx, account.ProvisionLocalParams{
		UserID: userID, Count: 5, TotalBytes: 1 << 30,
		MaxUploadWorkers: 2, MaxDownloadWorkers: 1,
	})
	if err != nil {
		t.Fatalf("provision local accounts: %v", err)
	}
	if len(provisioned) != 5 {
		t.Fatalf("provisioned account count = %d, want 5", len(provisioned))
	}
	localRoot := t.TempDir()
	localProvider, err := localstorage.New(localRoot)
	if err != nil {
		t.Fatalf("create local provider: %v", err)
	}
	for index, provisionedAccount := range provisioned {
		wantName := fmt.Sprintf("local-%d", index+1)
		if provisionedAccount.Name != wantName || provisionedAccount.Provider != "local" || provisionedAccount.TotalBytes != 1<<30 {
			t.Errorf("provisioned account %d = %+v, want %s local with 1 GiB", index, provisionedAccount, wantName)
		}
		if err := localProvider.Health(ctx, provisionedAccount); err != nil {
			t.Fatalf("initialize %s: %v", wantName, err)
		}
		if info, err := os.Stat(filepath.Join(localRoot, provisionedAccount.ID, "objects")); err != nil || !info.IsDir() {
			t.Fatalf("physical directory for %s: info=%v error=%v", wantName, info, err)
		}
	}
	reprovisioned, err := accounts.ProvisionLocal(ctx, account.ProvisionLocalParams{
		UserID: userID, Count: 5, TotalBytes: 2 << 30,
		MaxUploadWorkers: 3, MaxDownloadWorkers: 2,
	})
	if err != nil {
		t.Fatalf("reprovision local accounts: %v", err)
	}
	if len(reprovisioned) != 5 || reprovisioned[0].ID != provisioned[0].ID || reprovisioned[0].TotalBytes != 2<<30 {
		t.Fatalf("reprovisioned accounts are not idempotent: first=%+v second=%+v", provisioned, reprovisioned)
	}

	uploads := upload.NewRepository(pool)
	missingDirectory := "44444444-4444-4444-8444-444444444444"
	var filesBefore int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM files WHERE user_id = $1", userID).Scan(&filesBefore); err != nil {
		t.Fatalf("count files before rejected upload: %v", err)
	}
	if _, _, err := uploads.CreateWithFile(ctx, upload.CreateAggregateParams{
		UserID: userID, DirectoryID: &missingDirectory, Name: "rejected.bin",
		MIMEType: "application/octet-stream", SizeBytes: 1, ChunkSize: 32,
		ChunkCount: 1, ExpiresAt: time.Now().Add(time.Hour),
	}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("CreateWithFile() missing directory error = %v, want ErrNotFound", err)
	}
	var filesAfter int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM files WHERE user_id = $1", userID).Scan(&filesAfter); err != nil {
		t.Fatalf("count files after rejected upload: %v", err)
	}
	if filesAfter != filesBefore {
		t.Fatalf("rejected upload left a file row: before=%d after=%d", filesBefore, filesAfter)
	}

	aggregateFile, aggregateSession, err := uploads.CreateWithFile(ctx, upload.CreateAggregateParams{
		UserID: userID, Name: "aggregate.bin", MIMEType: "application/octet-stream",
		SizeBytes: 65, ChunkSize: 32, ChunkCount: 3, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateWithFile(): %v", err)
	}
	if aggregateFile.State != filedomain.StateUploading || aggregateSession.State != upload.StateUploading || aggregateSession.FileID != aggregateFile.ID {
		t.Fatalf("created upload aggregate = file %+v session %+v", aggregateFile, aggregateSession)
	}
	if _, err := uploads.GetForUser(ctx, aggregateSession.ID, "55555555-5555-4555-8555-555555555555"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-user GetForUser() error = %v, want ErrNotFound", err)
	}
	chunks := chunk.NewRepository(pool)
	fixedNow := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	registry := storage.NewRegistry()
	if err := registry.Register("local", localProvider); err != nil {
		t.Fatalf("register local provider: %v", err)
	}
	uploadService, err := upload.NewService(
		uploads, chunks, accounts, registry, placement.NewDefault(), upload.ServiceConfig{
			ChunkSize: 32, SessionLifetime: 24 * time.Hour,
			Now: func() time.Time { return fixedNow }, GlobalUploadLimit: 6,
		},
	)
	if err != nil {
		t.Fatalf("create upload service: %v", err)
	}
	created, err := uploadService.Create(ctx, userID, upload.CreateRequest{Name: "service.bin", SizeBytes: 65})
	if err != nil {
		t.Fatalf("service Create(): %v", err)
	}
	if created.File.ChunkCount != 3 || created.File.MIMEType != "application/octet-stream" ||
		created.Session.State != upload.StateUploading || !created.Session.ExpiresAt.Equal(fixedNow.Add(24*time.Hour)) {
		t.Fatalf("service-created upload = %+v / %+v", created.File, created.Session)
	}
	status, err := uploadService.Get(ctx, userID, created.Session.ID)
	if err != nil {
		t.Fatalf("service Get(): %v", err)
	}
	if status.CompletedIndexes == nil || len(status.CompletedIndexes) != 0 {
		t.Fatalf("initial completed indexes = %#v, want empty non-nil slice", status.CompletedIndexes)
	}
	serviceAccounts, err := accounts.ProvisionLocal(ctx, account.ProvisionLocalParams{
		UserID: userID, Count: 5, TotalBytes: 100,
		MaxUploadWorkers: 2, MaxDownloadWorkers: 1,
	})
	if err != nil {
		t.Fatalf("set integration account quotas: %v", err)
	}
	fillerID, err := storage.NewObjectID()
	if err != nil {
		t.Fatalf("generate filler object ID: %v", err)
	}
	if _, err := localProvider.Upload(ctx, serviceAccounts[0], storage.UploadRequest{
		ObjectID: fillerID, SizeBytes: 90, Body: strings.NewReader(strings.Repeat("x", 90)),
	}); err != nil {
		t.Fatalf("fill first local account: %v", err)
	}
	placementConfig := placement.DefaultConfig()
	placementConfig.MinimumReserveBytes = 0
	placementConfig.ReserveChunkMultiplier = 0
	placementEngine, err := placement.New(placementConfig)
	if err != nil {
		t.Fatalf("create integration placement engine: %v", err)
	}
	uploadService, err = upload.NewService(
		uploads, chunks, accounts, registry, placementEngine, upload.ServiceConfig{
			ChunkSize: 32, SessionLifetime: 24 * time.Hour,
			Now: func() time.Time { return fixedNow }, GlobalUploadLimit: 6,
		},
	)
	if err != nil {
		t.Fatalf("create upload service with integration placement: %v", err)
	}
	payloads := []string{strings.Repeat("a", 32), strings.Repeat("b", 32), "c"}
	uploadedResults := make([]upload.ChunkResult, len(payloads))
	selectedAccounts := make(map[string]struct{})
	accountsByID := make(map[string]account.Account, len(serviceAccounts))
	for _, serviceAccount := range serviceAccounts {
		accountsByID[serviceAccount.ID] = serviceAccount
	}
	for index, payload := range payloads {
		result, err := uploadService.UploadChunk(ctx, userID, created.Session.ID, index, strings.NewReader(payload), int64(len(payload)))
		if err != nil {
			t.Fatalf("UploadChunk(%d): %v", index, err)
		}
		if !result.Created || result.Index != index || result.SizeBytes != int64(len(payload)) || result.ChecksumSHA256 == "" {
			t.Fatalf("UploadChunk(%d) result = %+v", index, result)
		}
		uploadedResults[index] = result
		if index == 0 && result.StorageAccountID == serviceAccounts[0].ID {
			t.Fatal("first chunk did not retry away from provider-full account")
		}
		reader, err := localProvider.Download(ctx, accountsByID[result.StorageAccountID], result.RemoteObjectID)
		if err != nil {
			t.Fatalf("download uploaded chunk %d: %v", index, err)
		}
		downloaded, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil || closeErr != nil || string(downloaded) != payload {
			t.Fatalf("uploaded chunk %d content/read/close = %q / %v / %v", index, downloaded, readErr, closeErr)
		}
		selectedAccounts[result.StorageAccountID] = struct{}{}
	}
	if len(selectedAccounts) < 2 {
		t.Fatalf("uploaded chunks were not distributed: %v", selectedAccounts)
	}
	notRead := &failOnRead{}
	duplicate, err := uploadService.UploadChunk(ctx, userID, created.Session.ID, 0, notRead, 999)
	if err != nil {
		t.Fatalf("idempotent UploadChunk(): %v", err)
	}
	if duplicate.Created || notRead.read {
		t.Fatalf("duplicate result/read = %+v / %v", duplicate, notRead.read)
	}
	resumed, err := uploadService.Get(ctx, userID, created.Session.ID)
	if err != nil {
		t.Fatalf("Get() after chunk uploads: %v", err)
	}
	if len(resumed.CompletedIndexes) != 3 || resumed.CompletedIndexes[0] != 0 || resumed.CompletedIndexes[1] != 1 || resumed.CompletedIndexes[2] != 2 {
		t.Fatalf("completed indexes after upload = %v", resumed.CompletedIndexes)
	}
	wantWholeChecksum := sha256.Sum256([]byte(strings.Join(payloads, "")))
	completed, err := uploadService.Complete(ctx, userID, created.Session.ID)
	if err != nil {
		t.Fatalf("Complete(): %v", err)
	}
	if completed.State != filedomain.StateAvailable || completed.FileID != created.File.ID || completed.ChecksumSHA256 != hex.EncodeToString(wantWholeChecksum[:]) {
		t.Fatalf("completed upload = %+v", completed)
	}
	idempotentCompletion, err := uploadService.Complete(ctx, userID, created.Session.ID)
	if err != nil || idempotentCompletion != completed {
		t.Fatalf("idempotent Complete() = %+v, %v; want %+v", idempotentCompletion, err, completed)
	}
	completedSession, err := uploads.Get(ctx, created.Session.ID)
	if err != nil || completedSession.State != upload.StateCompleted {
		t.Fatalf("completed session = %+v, %v", completedSession, err)
	}
	completedFile, err := filedomain.NewRepository(pool).Get(ctx, created.File.ID)
	if err != nil || completedFile.State != filedomain.StateAvailable || completedFile.ChecksumSHA256 == nil || *completedFile.ChecksumSHA256 != completed.ChecksumSHA256 {
		t.Fatalf("completed file = %+v, %v", completedFile, err)
	}
	downloadService, err := download.NewService(filedomain.NewRepository(pool), chunks, accounts, registry)
	if err != nil {
		t.Fatalf("create download service: %v", err)
	}
	downloadPlan, err := downloadService.Prepare(ctx, userID, created.File.ID)
	if err != nil {
		t.Fatalf("prepare download: %v", err)
	}
	var downloaded bytes.Buffer
	if err := downloadService.Stream(ctx, downloadPlan, &downloaded); err != nil {
		t.Fatalf("stream download: %v", err)
	}
	if downloaded.String() != strings.Join(payloads, "") || sha256.Sum256(downloaded.Bytes()) != wantWholeChecksum {
		t.Fatalf("downloaded bytes/checksum do not match original")
	}
	downloadHandler := download.NewHandler(downloadService, userID)
	router := httpapi.NewRouter(health.NewHandler(pool, time.Second), nil, downloadHandler)
	httpResponse := httptest.NewRecorder()
	router.ServeHTTP(httpResponse, httptest.NewRequest(http.MethodGet, "/api/v1/files/"+created.File.ID+"/download", nil))
	if httpResponse.Code != http.StatusOK || httpResponse.Body.String() != strings.Join(payloads, "") {
		t.Fatalf("HTTP download = %d %q", httpResponse.Code, httpResponse.Body.String())
	}
	if httpResponse.Header().Get("Content-Length") != "65" || httpResponse.Header().Get("Content-Type") != "application/octet-stream" ||
		!strings.HasPrefix(httpResponse.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("HTTP download headers = %#v", httpResponse.Header())
	}

	recreatedProvider, err := localstorage.New(localRoot)
	if err != nil {
		t.Fatalf("recreate local provider: %v", err)
	}
	recreatedRegistry := storage.NewRegistry()
	if err := recreatedRegistry.Register("local", recreatedProvider); err != nil {
		t.Fatalf("register recreated local provider: %v", err)
	}
	recreatedDownloadService, err := download.NewService(
		filedomain.NewRepository(pool), chunk.NewRepository(pool), account.NewRepository(pool), recreatedRegistry,
	)
	if err != nil {
		t.Fatalf("recreate download service: %v", err)
	}
	if _, err := recreatedDownloadService.Prepare(ctx, "55555555-5555-4555-8555-555555555555", created.File.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-user download prepare error = %v, want ErrNotFound", err)
	}
	recreatedPlan, err := recreatedDownloadService.Prepare(ctx, userID, created.File.ID)
	if err != nil {
		t.Fatalf("prepare download after service recreation: %v", err)
	}
	downloaded.Reset()
	if err := recreatedDownloadService.Stream(ctx, recreatedPlan, &downloaded); err != nil {
		t.Fatalf("stream download after service recreation: %v", err)
	}
	if downloaded.String() != strings.Join(payloads, "") {
		t.Fatal("download after service recreation differs from original")
	}

	firstResult := uploadedResults[0]
	firstObjectPath := filepath.Join(localRoot, firstResult.StorageAccountID, "objects", firstResult.RemoteObjectID[:2], firstResult.RemoteObjectID)
	if err := os.WriteFile(firstObjectPath, []byte(strings.Repeat("d", len(payloads[0]))), 0o640); err != nil {
		t.Fatalf("corrupt completed-file object: %v", err)
	}
	corruptDownloadPlan, err := recreatedDownloadService.Prepare(ctx, userID, created.File.ID)
	if err != nil {
		t.Fatalf("same-size corruption should pass metadata preparation: %v", err)
	}
	downloaded.Reset()
	if err := recreatedDownloadService.Stream(ctx, corruptDownloadPlan, &downloaded); !errors.Is(err, download.ErrIntegrity) {
		t.Fatalf("corrupt download error = %v, want ErrIntegrity", err)
	}
	if downloaded.Len() != 0 {
		t.Fatalf("corrupt first chunk wrote %d bytes before verification", downloaded.Len())
	}
	if err := os.WriteFile(firstObjectPath, []byte(payloads[0]), 0o640); err != nil {
		t.Fatalf("restore completed-file object: %v", err)
	}
	secondResult := uploadedResults[1]
	if err := recreatedProvider.Delete(ctx, accountsByID[secondResult.StorageAccountID], secondResult.RemoteObjectID); err != nil {
		t.Fatalf("remove completed-file object: %v", err)
	}
	if _, err := recreatedDownloadService.Prepare(ctx, userID, created.File.ID); !errors.Is(err, download.ErrIntegrity) {
		t.Fatalf("missing-object prepare error = %v, want ErrIntegrity", err)
	}

	incompleteUpload, err := uploadService.Create(ctx, userID, upload.CreateRequest{Name: "incomplete.bin", SizeBytes: 32})
	if err != nil {
		t.Fatalf("create incomplete upload: %v", err)
	}
	if _, err := uploadService.Complete(ctx, userID, incompleteUpload.Session.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("incomplete Complete() error = %v, want ErrConflict", err)
	}
	if _, err := downloadService.Prepare(ctx, userID, incompleteUpload.File.ID); !errors.Is(err, domain.ErrInvalidState) {
		t.Fatalf("incomplete file download error = %v, want ErrInvalidState", err)
	}
	incompleteSession, err := uploads.Get(ctx, incompleteUpload.Session.ID)
	if err != nil || incompleteSession.State != upload.StateUploading {
		t.Fatalf("incomplete session changed = %+v, %v", incompleteSession, err)
	}

	corruptUpload, err := uploadService.Create(ctx, userID, upload.CreateRequest{Name: "corrupt.bin", SizeBytes: 1})
	if err != nil {
		t.Fatalf("create corrupt upload: %v", err)
	}
	corruptResult, err := uploadService.UploadChunk(ctx, userID, corruptUpload.Session.ID, 0, strings.NewReader("z"), 1)
	if err != nil {
		t.Fatalf("upload corrupt-test chunk: %v", err)
	}
	corruptObjectPath := filepath.Join(localRoot, corruptResult.StorageAccountID, "objects", corruptResult.RemoteObjectID[:2], corruptResult.RemoteObjectID)
	if err := os.WriteFile(corruptObjectPath, []byte("y"), 0o640); err != nil {
		t.Fatalf("corrupt remote object content: %v", err)
	}
	if _, err := uploadService.Complete(ctx, userID, corruptUpload.Session.ID); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("corrupt Complete() error = %v, want ErrConflict", err)
	}
	corruptChunk, err := chunks.GetByIndex(ctx, corruptUpload.File.ID, 0)
	if err != nil || corruptChunk.State != chunk.StateCorrupt {
		t.Fatalf("corrupt chunk state = %+v, %v", corruptChunk, err)
	}
	corruptSession, err := uploads.Get(ctx, corruptUpload.Session.ID)
	if err != nil || corruptSession.State != upload.StateFailed {
		t.Fatalf("corrupt session state = %+v, %v", corruptSession, err)
	}
	corruptFile, err := filedomain.NewRepository(pool).Get(ctx, corruptUpload.File.ID)
	if err != nil || corruptFile.State != filedomain.StateFailed {
		t.Fatalf("corrupt file state = %+v, %v", corruptFile, err)
	}

	emptyUpload, err := uploadService.Create(ctx, userID, upload.CreateRequest{Name: "empty.bin", SizeBytes: 0})
	if err != nil {
		t.Fatalf("create empty upload: %v", err)
	}
	emptyCompletion, err := uploadService.Complete(ctx, userID, emptyUpload.Session.ID)
	if err != nil {
		t.Fatalf("complete empty upload: %v", err)
	}
	wantEmptyChecksum := sha256.Sum256(nil)
	if emptyCompletion.State != filedomain.StateAvailable || emptyCompletion.ChecksumSHA256 != hex.EncodeToString(wantEmptyChecksum[:]) {
		t.Fatalf("empty completion = %+v", emptyCompletion)
	}
	emptyPlan, err := downloadService.Prepare(ctx, userID, emptyUpload.File.ID)
	if err != nil {
		t.Fatalf("prepare empty download: %v", err)
	}
	downloaded.Reset()
	if err := downloadService.Stream(ctx, emptyPlan, &downloaded); err != nil || downloaded.Len() != 0 {
		t.Fatalf("empty download = %d bytes, %v", downloaded.Len(), err)
	}

	concurrentUpload, err := uploadService.Create(ctx, userID, upload.CreateRequest{Name: "concurrent-complete.bin", SizeBytes: 1})
	if err != nil {
		t.Fatalf("create concurrent completion upload: %v", err)
	}
	if _, err := uploadService.UploadChunk(ctx, userID, concurrentUpload.Session.ID, 0, strings.NewReader("q"), 1); err != nil {
		t.Fatalf("upload concurrent completion chunk: %v", err)
	}
	const completers = 4
	completionStart := make(chan struct{})
	completionErrors := make(chan error, completers)
	var completionWait sync.WaitGroup
	for range completers {
		completionWait.Add(1)
		go func() {
			defer completionWait.Done()
			<-completionStart
			result, err := uploadService.Complete(ctx, userID, concurrentUpload.Session.ID)
			if err == nil && (result.State != filedomain.StateAvailable || result.ChecksumSHA256 == "") {
				err = fmt.Errorf("unexpected concurrent completion result: %+v", result)
			}
			completionErrors <- err
		}()
	}
	close(completionStart)
	completionWait.Wait()
	close(completionErrors)
	for err := range completionErrors {
		if err != nil {
			t.Errorf("concurrent Complete() error = %v", err)
		}
	}

	bodyErrorUpload, err := uploadService.Create(ctx, userID, upload.CreateRequest{Name: "body-error.bin", SizeBytes: 32})
	if err != nil {
		t.Fatalf("create body-error upload: %v", err)
	}
	bodyError := &errorReader{}
	if _, err := uploadService.UploadChunk(ctx, userID, bodyErrorUpload.Session.ID, 0, bodyError, -1); err == nil {
		t.Fatal("UploadChunk() body error = nil")
	}
	if bodyError.reads != 1 {
		t.Fatalf("body error reader reads = %d, want 1 without provider retry", bodyError.reads)
	}
	wrongLengthUpload, err := uploadService.Create(ctx, userID, upload.CreateRequest{Name: "wrong-length.bin", SizeBytes: 32})
	if err != nil {
		t.Fatalf("create wrong-length upload: %v", err)
	}
	wrongLengthBody := &failOnRead{}
	if _, err := uploadService.UploadChunk(ctx, userID, wrongLengthUpload.Session.ID, 0, wrongLengthBody, 31); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("UploadChunk() wrong content length error = %v, want ErrInvalid", err)
	}
	if wrongLengthBody.read {
		t.Fatal("wrong content length consumed request body")
	}

	files := filedomain.NewRepository(pool)
	storedFile, err := files.Create(ctx, filedomain.CreateParams{
		UserID: userID, Name: "payload.bin", MIMEType: "application/octet-stream",
		SizeBytes: 10, ChunkSize: 10, ChunkCount: 1,
	})
	if err != nil {
		t.Fatalf("create file: %v", err)
	}

	session, err := uploads.Create(ctx, upload.CreateParams{
		UserID: userID, FileID: storedFile.ID, ExpectedSize: 10,
		ExpectedChunks: 1, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create upload: %v", err)
	}
	session, err = uploads.Transition(ctx, session.ID, upload.StateCreating, upload.StateUploading)
	if err != nil {
		t.Fatalf("start upload: %v", err)
	}
	var usageBeforeRace int64
	if err := pool.QueryRow(ctx, "SELECT used_bytes FROM storage_accounts WHERE id = $1", storedAccount.ID).Scan(&usageBeforeRace); err != nil {
		t.Fatalf("read account usage before race: %v", err)
	}

	params := chunk.StoreParams{
		UploadID: session.ID, FileID: storedFile.ID, Index: 0, SizeBytes: 10,
		ChecksumSHA256:   "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		StorageAccountID: storedAccount.ID, RemoteObjectID: "opaque-object-id",
	}

	const racers = 8
	start := make(chan struct{})
	errorsSeen := make(chan error, racers)
	var createdCount atomic.Int32
	var wg sync.WaitGroup
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, created, err := chunks.Store(ctx, params)
			if created {
				createdCount.Add(1)
			}
			errorsSeen <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Errorf("concurrent Store() error = %v", err)
		}
	}
	if got := createdCount.Load(); got != 1 {
		t.Errorf("created count = %d, want 1", got)
	}
	var mappedUsage int64
	if err := pool.QueryRow(ctx, "SELECT used_bytes FROM storage_accounts WHERE id = $1", storedAccount.ID).Scan(&mappedUsage); err != nil {
		t.Fatalf("read mapped account usage: %v", err)
	}
	if mappedUsage != usageBeforeRace+10 {
		t.Fatalf("mapped account usage = %d, want %d", mappedUsage, usageBeforeRace+10)
	}

	session, err = uploads.Get(ctx, session.ID)
	if err != nil {
		t.Fatalf("get upload: %v", err)
	}
	if session.CompletedChunks != 1 || session.ReceivedBytes != 10 {
		t.Errorf("upload progress = %d chunks/%d bytes, want 1/10", session.CompletedChunks, session.ReceivedBytes)
	}
	resumedStatus, err := uploadService.Get(ctx, userID, session.ID)
	if err != nil {
		t.Fatalf("get resumed upload status: %v", err)
	}
	if len(resumedStatus.CompletedIndexes) != 1 || resumedStatus.CompletedIndexes[0] != 0 {
		t.Fatalf("resumed completed indexes = %v, want [0]", resumedStatus.CompletedIndexes)
	}
	ordered, err := chunks.ListByFile(ctx, storedFile.ID)
	if err != nil || len(ordered) != 1 || ordered[0].Index != 0 {
		t.Fatalf("list chunks = %+v, %v", ordered, err)
	}

	conflicting := params
	conflicting.ChecksumSHA256 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if _, _, err := chunks.Store(ctx, conflicting); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("conflicting Store() error = %v, want ErrConflict", err)
	}

	if _, err := files.Transition(ctx, storedFile.ID, filedomain.StateAvailable, filedomain.StateDeleting); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("stale file transition error = %v, want ErrConflict", err)
	}
	if _, err := accounts.Get(ctx, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing account error = %v, want ErrNotFound", err)
	}
}

type failOnRead struct{ read bool }

func (r *failOnRead) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("reader must not be consumed")
}

type errorReader struct{ reads int }

func (r *errorReader) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("request body failed")
}

func newTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("SHARDRIVE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SHARDRIVE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := db.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}

	databaseName := fmt.Sprintf("shardrive_repository_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		admin.Close()
		t.Fatalf("create test database: %v", err)
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		admin.Close()
		t.Fatalf("parse database URL: %v", err)
	}
	config.ConnConfig.Database = databaseName
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		admin.Close()
		t.Fatalf("connect test database: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		pool.Close()
		admin.Close()
		t.Fatalf("migrate test database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
		admin.Close()
	})
	return pool
}
