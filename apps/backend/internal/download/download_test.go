package download

import (
	"errors"
	"testing"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

func TestServiceValidationAndAccountLimit(t *testing.T) {
	service, err := NewService(
		filedomain.NewRepository(nil), chunk.NewRepository(nil), account.NewRepository(nil), storage.NewRegistry(),
	)
	if err != nil {
		t.Fatalf("NewService(): %v", err)
	}
	if got := cap(service.accountLimit(account.Account{ID: "account-1", MaxDownloadWorkers: 2})); got != 2 {
		t.Fatalf("account download limit = %d, want 2", got)
	}
	if got := cap(service.accountLimit(account.Account{ID: "account-2", MaxDownloadWorkers: 0})); got != 1 {
		t.Fatalf("invalid account download limit fallback = %d, want 1", got)
	}
	if _, err := service.Prepare(t.Context(), "invalid", "invalid"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Prepare() invalid ID error = %v, want ErrInvalid", err)
	}
}

func TestValidFileLayoutAndSizeAt(t *testing.T) {
	storedFile := filedomain.File{SizeBytes: 65, ChunkSize: 32, ChunkCount: 3}
	for index, want := range []int64{32, 32, 1} {
		got, err := sizeAt(storedFile, index)
		if err != nil || got != want {
			t.Fatalf("sizeAt(%d) = %d, %v; want %d", index, got, err, want)
		}
	}
	if !validFileLayout(filedomain.File{SizeBytes: 0, ChunkSize: 32, ChunkCount: 0}) {
		t.Fatal("zero-byte file layout is invalid")
	}
	for _, invalid := range []filedomain.File{
		{SizeBytes: -1, ChunkSize: 32, ChunkCount: 0},
		{SizeBytes: 1, ChunkSize: 0, ChunkCount: 1},
		{SizeBytes: 65, ChunkSize: 32, ChunkCount: 2},
	} {
		if validFileLayout(invalid) {
			t.Fatalf("validFileLayout(%+v) = true", invalid)
		}
	}
}

func TestIntegrityErrorPreservesSentinel(t *testing.T) {
	if err := integrityError("chunk %d", 2); !errors.Is(err, ErrIntegrity) {
		t.Fatalf("integrityError() = %v", err)
	}
}
