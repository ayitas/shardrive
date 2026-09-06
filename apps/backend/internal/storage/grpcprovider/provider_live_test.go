package grpcprovider

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

func TestLiveProtonAdapterTransferGate(t *testing.T) {
	address := os.Getenv("SHARDRIVE_LIVE_GRPC_ADDRESS")
	accountID := os.Getenv("SHARDRIVE_LIVE_ACCOUNT_ID")
	if address == "" || accountID == "" {
		t.Skip("set SHARDRIVE_LIVE_GRPC_ADDRESS and SHARDRIVE_LIVE_ACCOUNT_ID for the live adapter gate")
	}

	provider, err := New(address)
	if err != nil {
		t.Fatal(err)
	}
	defer provider.Close()
	ctx := context.Background()
	storageAccount := account.Account{ID: accountID}
	if err := provider.Health(ctx, storageAccount); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if _, err := provider.Usage(ctx, storageAccount); err != nil {
		t.Fatalf("Usage: %v", err)
	}

	payload := []byte("Shardrive Go to Proton gRPC transfer gate\n")
	objectID := "shardrive-live-" + randomSuffix(t)
	remote, err := provider.Upload(ctx, storageAccount, storage.UploadRequest{
		ObjectID: objectID, SizeBytes: int64(len(payload)), Body: strings.NewReader(string(payload)),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	defer func() {
		if deleteErr := provider.Delete(ctx, storageAccount, remote.ObjectID); deleteErr != nil {
			t.Errorf("Delete: %v", deleteErr)
		}
	}()

	reader, err := provider.Download(ctx, storageAccount, remote.ObjectID)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	downloaded, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil {
		t.Fatalf("read download: %v", readErr)
	}
	if closeErr != nil {
		t.Fatalf("close download: %v", closeErr)
	}
	originalHash := sha256.Sum256(payload)
	downloadedHash := sha256.Sum256(downloaded)
	if originalHash != downloadedHash {
		t.Fatalf("checksum mismatch: original=%s downloaded=%s", hex.EncodeToString(originalHash[:]), hex.EncodeToString(downloadedHash[:]))
	}
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	bytes := make([]byte, 12)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(bytes)
}
