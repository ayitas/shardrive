package job

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
)

type cleanupPayload struct {
	FileID string `json:"fileId"`
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return fmt.Errorf("worker interval must be positive")
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := w.ProcessOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Empty queues and retryable job failures do not stop the worker loop.
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

type cleanupChunks interface {
	ListByFile(context.Context, string) ([]chunk.Chunk, error)
	MarkDeleted(context.Context, string) error
}
type cleanupAccounts interface {
	Get(context.Context, string) (account.Account, error)
}
type cleanupFiles interface {
	Get(context.Context, string) (filedomain.File, error)
	Transition(context.Context, string, filedomain.State, filedomain.State) (filedomain.File, error)
}
type Worker struct {
	jobs      *Repository
	chunks    cleanupChunks
	accounts  cleanupAccounts
	providers *storage.Registry
	workerID  string
	files     cleanupFiles
}

func NewCleanupWorker(jobs *Repository, chunks cleanupChunks, accounts cleanupAccounts, providers *storage.Registry, workerID string, files ...cleanupFiles) *Worker {
	var fileRepo cleanupFiles
	if len(files) > 0 {
		fileRepo = files[0]
	}
	return &Worker{jobs: jobs, chunks: chunks, accounts: accounts, providers: providers, workerID: workerID, files: fileRepo}
}

func (w *Worker) ProcessOnce(ctx context.Context) error {
	claimed, err := w.jobs.Claim(ctx, w.workerID)
	if err != nil {
		return err
	}
	var payload cleanupPayload
	if err := json.Unmarshal([]byte(claimed.Payload), &payload); err != nil {
		w.retryCleanup(ctx, claimed, "", err.Error())
		return fmt.Errorf("decode cleanup payload: %w", err)
	}
	values, err := w.chunks.ListByFile(ctx, payload.FileID)
	if err != nil {
		w.retryCleanup(ctx, claimed, payload.FileID, err.Error())
		return err
	}
	for _, value := range values {
		if value.State == chunk.StateDeleted {
			continue
		}
		if value.StorageAccountID == nil || value.RemoteObjectID == nil {
			w.retryCleanup(ctx, claimed, payload.FileID, "chunk has incomplete remote mapping")
			return fmt.Errorf("chunk %s has incomplete remote mapping", value.ID)
		}
		storageAccount, err := w.accounts.Get(ctx, *value.StorageAccountID)
		if err != nil {
			w.retryCleanup(ctx, claimed, payload.FileID, err.Error())
			return err
		}
		provider, err := w.providers.Get(storageAccount.Provider)
		if err != nil {
			w.retryCleanup(ctx, claimed, payload.FileID, err.Error())
			return err
		}
		if err := provider.Delete(ctx, storageAccount, *value.RemoteObjectID); err != nil {
			w.retryCleanup(ctx, claimed, payload.FileID, err.Error())
			return err
		}
		if err := w.chunks.MarkDeleted(ctx, value.ID); err != nil {
			w.retryCleanup(ctx, claimed, payload.FileID, err.Error())
			return err
		}
	}
	if w.files != nil {
		value, err := w.files.Get(ctx, payload.FileID)
		if err != nil {
			w.retryCleanup(ctx, claimed, payload.FileID, err.Error())
			return err
		}
		if value.State == filedomain.StateDeleting {
			if _, err := w.files.Transition(ctx, value.ID, value.State, filedomain.StateDeleted); err != nil {
				w.retryCleanup(ctx, claimed, payload.FileID, err.Error())
				return err
			}
		}
	}
	return w.jobs.Complete(ctx, claimed.ID)
}

func (w *Worker) retryCleanup(ctx context.Context, claimed Job, fileID, message string) {
	_ = w.jobs.Retry(ctx, claimed.ID, message)
	if fileID == "" || w.files == nil || claimed.Attempts < claimed.MaxAttempts {
		return
	}
	file, err := w.files.Get(ctx, fileID)
	if err == nil && file.State == filedomain.StateDeleting {
		_, _ = w.files.Transition(ctx, file.ID, file.State, filedomain.StateFailed)
	}
}
