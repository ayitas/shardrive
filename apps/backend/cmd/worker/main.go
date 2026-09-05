package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/config"
	"github.com/ayitas/shardrive/apps/backend/internal/db"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/job"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	localstorage "github.com/ayitas/shardrive/apps/backend/internal/storage/local"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	provider, err := localstorage.New(cfg.LocalStorageRoot)
	if err != nil {
		return err
	}
	providers := storage.NewRegistry()
	if err := providers.Register("local", provider); err != nil {
		return err
	}
	worker := job.NewCleanupWorker(job.NewRepository(pool), chunk.NewRepository(pool), account.NewRepository(pool), providers, "worker-1", filedomain.NewRepository(pool))
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	logger.Info("cleanup worker started")
	return worker.Run(signalCtx, 2*time.Second)
}
