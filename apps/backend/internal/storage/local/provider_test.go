package local

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

func TestProviderRoundTripUsageAndDelete(t *testing.T) {
	root := t.TempDir()
	provider := newProvider(t, root)
	storageAccount := testAccount(t, 1<<20)
	objectID := newID(t)
	payload := bytes.Repeat([]byte("shardrive-local-provider"), 4096)

	stored, err := provider.Upload(context.Background(), storageAccount, storage.UploadRequest{
		ObjectID: objectID, SizeBytes: int64(len(payload)), Body: bytes.NewReader(payload),
	})
	if err != nil {
		t.Fatalf("Upload(): %v", err)
	}
	if stored.ObjectID != objectID || stored.SizeBytes != int64(len(payload)) {
		t.Fatalf("stored object = %+v", stored)
	}
	expectedPath := filepath.Join(root, storageAccount.ID, "objects", objectID[:2], objectID)
	if _, err := os.Stat(expectedPath); err != nil {
		t.Fatalf("stat physical object: %v", err)
	}

	info, err := provider.Stat(context.Background(), storageAccount, objectID)
	if err != nil {
		t.Fatalf("Stat(): %v", err)
	}
	if info.SizeBytes != int64(len(payload)) || info.ObjectID != objectID {
		t.Fatalf("ObjectInfo = %+v", info)
	}

	reader, err := provider.Download(context.Background(), storageAccount, objectID)
	if err != nil {
		t.Fatalf("Download(): %v", err)
	}
	downloaded, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil {
		t.Fatalf("read/close download: %v / %v", readErr, closeErr)
	}
	if !bytes.Equal(downloaded, payload) {
		t.Fatal("downloaded bytes differ from uploaded bytes")
	}

	usage, err := provider.Usage(context.Background(), storageAccount)
	if err != nil {
		t.Fatalf("Usage(): %v", err)
	}
	if usage.UsedBytes != int64(len(payload)) || usage.FreeBytes != storageAccount.TotalBytes-int64(len(payload)) {
		t.Fatalf("Usage() = %+v", usage)
	}
	if err := provider.Delete(context.Background(), storageAccount, objectID); err != nil {
		t.Fatalf("Delete(): %v", err)
	}
	if err := provider.Delete(context.Background(), storageAccount, objectID); err != nil {
		t.Fatalf("idempotent Delete(): %v", err)
	}
	if _, err := provider.Stat(context.Background(), storageAccount, objectID); !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("Stat() after delete error = %v, want ErrObjectNotFound", err)
	}
}

func TestProviderIsolatesAccounts(t *testing.T) {
	provider := newProvider(t, t.TempDir())
	first := testAccount(t, 100)
	second := testAccount(t, 100)
	objectID := newID(t)
	upload(t, provider, first, objectID, "first")
	upload(t, provider, second, objectID, "second")
	if got := download(t, provider, first, objectID); got != "first" {
		t.Errorf("first account content = %q", got)
	}
	if got := download(t, provider, second, objectID); got != "second" {
		t.Errorf("second account content = %q", got)
	}
}

func TestProviderEnforcesQuotaAcrossConcurrentUploads(t *testing.T) {
	provider := newProvider(t, t.TempDir())
	storageAccount := testAccount(t, 10)
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := provider.Upload(context.Background(), storageAccount, storage.UploadRequest{
				ObjectID: newID(t), SizeBytes: 6, Body: strings.NewReader("123456"),
			})
			results <- err
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var successes, quotaErrors int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, storage.ErrQuotaExceeded) {
			quotaErrors++
		} else {
			t.Errorf("Upload() error = %v", err)
		}
	}
	if successes != 1 || quotaErrors != 1 {
		t.Fatalf("successes/quota errors = %d/%d, want 1/1", successes, quotaErrors)
	}
	usage, err := provider.Usage(context.Background(), storageAccount)
	if err != nil || usage.UsedBytes != 6 {
		t.Fatalf("Usage() = %+v, %v; want 6 used", usage, err)
	}
}

func TestProviderRejectsWrongSizeWithoutPartialObject(t *testing.T) {
	provider := newProvider(t, t.TempDir())
	storageAccount := testAccount(t, 100)
	for _, test := range []struct {
		name, body string
		size       int64
	}{
		{"short", "123", 4}, {"long", "12345", 4},
	} {
		t.Run(test.name, func(t *testing.T) {
			objectID := newID(t)
			_, err := provider.Upload(context.Background(), storageAccount, storage.UploadRequest{ObjectID: objectID, SizeBytes: test.size, Body: strings.NewReader(test.body)})
			if !errors.Is(err, storage.ErrInvalidRequest) {
				t.Fatalf("Upload() error = %v, want ErrInvalidRequest", err)
			}
			if _, err := provider.Stat(context.Background(), storageAccount, objectID); !errors.Is(err, storage.ErrObjectNotFound) {
				t.Fatalf("partial object exists: %v", err)
			}
		})
	}
}

func TestProviderNeverOverwritesCommittedObject(t *testing.T) {
	provider := newProvider(t, t.TempDir())
	storageAccount := testAccount(t, 100)
	objectID := newID(t)
	upload(t, provider, storageAccount, objectID, "original")
	_, err := provider.Upload(context.Background(), storageAccount, storage.UploadRequest{ObjectID: objectID, SizeBytes: 11, Body: strings.NewReader("replacement")})
	if !errors.Is(err, storage.ErrObjectExists) {
		t.Fatalf("second Upload() error = %v, want ErrObjectExists", err)
	}
	if got := download(t, provider, storageAccount, objectID); got != "original" {
		t.Fatalf("object was overwritten with %q", got)
	}
}

func TestProviderHealthAndCancellation(t *testing.T) {
	provider := newProvider(t, t.TempDir())
	storageAccount := testAccount(t, 100)
	if err := provider.Health(context.Background(), storageAccount); err != nil {
		t.Fatalf("Health(): %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := provider.Upload(cancelled, storageAccount, storage.UploadRequest{ObjectID: newID(t), SizeBytes: 1, Body: strings.NewReader("x")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Upload() error = %v", err)
	}
	if _, err := provider.Download(context.Background(), storageAccount, "../unsafe"); !errors.Is(err, storage.ErrInvalidRequest) {
		t.Fatalf("unsafe Download() error = %v, want ErrInvalidRequest", err)
	}
}

func newProvider(t *testing.T, root string) *Provider {
	t.Helper()
	provider, err := New(root)
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	return provider
}
func newID(t *testing.T) string {
	t.Helper()
	id, err := storage.NewObjectID()
	if err != nil {
		t.Fatalf("NewObjectID(): %v", err)
	}
	return id
}
func testAccount(t *testing.T, quota int64) account.Account {
	t.Helper()
	return account.Account{ID: newID(t), Provider: "local", State: account.StateActive, TotalBytes: quota}
}
func upload(t *testing.T, provider *Provider, storageAccount account.Account, objectID, value string) {
	t.Helper()
	if _, err := provider.Upload(context.Background(), storageAccount, storage.UploadRequest{ObjectID: objectID, SizeBytes: int64(len(value)), Body: strings.NewReader(value)}); err != nil {
		t.Fatalf("Upload(): %v", err)
	}
}
func download(t *testing.T, provider *Provider, storageAccount account.Account, objectID string) string {
	t.Helper()
	reader, err := provider.Download(context.Background(), storageAccount, objectID)
	if err != nil {
		t.Fatalf("Download(): %v", err)
	}
	defer reader.Close()
	value, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read download: %v", err)
	}
	return string(value)
}
