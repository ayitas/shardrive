package upload

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

func TestServiceUploadLimits(t *testing.T) {
	service, err := NewService(
		NewRepository(nil), chunk.NewRepository(nil), account.NewRepository(nil),
		storage.NewRegistry(), placement.NewDefault(), ServiceConfig{
			ChunkSize: 32, SessionLifetime: time.Hour, GlobalUploadLimit: 3,
		},
	)
	if err != nil {
		t.Fatalf("NewService(): %v", err)
	}
	if got := cap(service.globalUploads); got != 3 {
		t.Fatalf("global upload limit = %d, want 3", got)
	}
	if got := cap(service.accountLimit(account.Account{ID: "account-1", MaxUploadWorkers: 2})); got != 2 {
		t.Fatalf("per-account upload limit = %d, want 2", got)
	}
	if got := cap(service.accountLimit(account.Account{ID: "account-2", MaxUploadWorkers: 0})); got != 1 {
		t.Fatalf("invalid per-account upload limit fallback = %d, want 1", got)
	}
}

func TestCompleteRejectsInvalidIDsBeforeRepositoryAccess(t *testing.T) {
	service := &Service{}
	if _, err := service.Complete(t.Context(), "invalid", "invalid"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("Complete() error = %v, want ErrInvalid", err)
	}
}

func TestChunkCount(t *testing.T) {
	tests := []struct {
		name      string
		size      int64
		chunkSize int64
		want      int
	}{
		{"empty", 0, 32, 0},
		{"one byte", 1, 32, 1},
		{"exact", 64, 32, 2},
		{"final partial", 65, 32, 3},
		{"no addition overflow", math.MaxInt64, math.MaxInt64, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ChunkCount(test.size, test.chunkSize)
			if err != nil || got != test.want {
				t.Fatalf("ChunkCount(%d, %d) = %d, %v; want %d", test.size, test.chunkSize, got, err, test.want)
			}
		})
	}
}

func TestChunkCountRejectsInvalidAndDatabaseOverflow(t *testing.T) {
	for _, test := range []struct{ size, chunk int64 }{{-1, 32}, {1, 0}, {math.MaxInt32 + 1, 1}} {
		if _, err := ChunkCount(test.size, test.chunk); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("ChunkCount(%d, %d) error = %v, want ErrInvalid", test.size, test.chunk, err)
		}
	}
}

func TestSizeAt(t *testing.T) {
	tests := []struct {
		index int
		want  int64
	}{{0, 32}, {1, 32}, {2, 1}}
	for _, test := range tests {
		got, err := SizeAt(65, 32, test.index)
		if err != nil || got != test.want {
			t.Errorf("SizeAt(65, 32, %d) = %d, %v; want %d", test.index, got, err, test.want)
		}
	}
	for _, index := range []int{-1, 3} {
		if _, err := SizeAt(65, 32, index); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("SizeAt index %d error = %v", index, err)
		}
	}
}
