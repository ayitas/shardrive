// Package storage defines the provider-neutral boundary used by Shardrive's
// upload, download, integrity, and cleanup services.
package storage

import (
	"context"
	"io"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
)

// Provider treats remote storage as an opaque object store. Implementations
// must honor context cancellation and must not close UploadRequest.Body.
type Provider interface {
	Upload(context.Context, account.Account, UploadRequest) (StoredObject, error)
	Download(context.Context, account.Account, string) (io.ReadCloser, error)
	Delete(context.Context, account.Account, string) error
	Stat(context.Context, account.Account, string) (ObjectInfo, error)
	Usage(context.Context, account.Account) (StorageUsage, error)
	Health(context.Context, account.Account) error
}

type UploadRequest struct {
	ObjectID  string
	SizeBytes int64
	Body      io.Reader
}

type StoredObject struct {
	ObjectID  string
	SizeBytes int64
}

type ObjectInfo struct {
	ObjectID   string
	SizeBytes  int64
	ModifiedAt time.Time
}

type StorageUsage struct {
	TotalBytes int64
	UsedBytes  int64
	FreeBytes  int64
}
