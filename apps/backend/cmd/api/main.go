package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ayitas/shardrive/apps/backend/internal/account"
	"github.com/ayitas/shardrive/apps/backend/internal/auth"
	"github.com/ayitas/shardrive/apps/backend/internal/chunk"
	"github.com/ayitas/shardrive/apps/backend/internal/config"
	"github.com/ayitas/shardrive/apps/backend/internal/db"
	"github.com/ayitas/shardrive/apps/backend/internal/directory"
	"github.com/ayitas/shardrive/apps/backend/internal/download"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/ayitas/shardrive/apps/backend/internal/health"
	"github.com/ayitas/shardrive/apps/backend/internal/httpapi"
	"github.com/ayitas/shardrive/apps/backend/internal/job"
	"github.com/ayitas/shardrive/apps/backend/internal/placement"
	"github.com/ayitas/shardrive/apps/backend/internal/storage"
	grpcstorage "github.com/ayitas/shardrive/apps/backend/internal/storage/grpcprovider"
	localstorage "github.com/ayitas/shardrive/apps/backend/internal/storage/local"
	"github.com/ayitas/shardrive/apps/backend/internal/upload"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		if err := runHealthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "bootstrap-user" {
		if err := bootstrapUser(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func bootstrapUser() error {
	email := strings.TrimSpace(os.Getenv("SHARDRIVE_BOOTSTRAP_EMAIL"))
	password := os.Getenv("SHARDRIVE_BOOTSTRAP_PASSWORD")
	if email == "" || password == "" {
		return errors.New("SHARDRIVE_BOOTSTRAP_EMAIL and SHARDRIVE_BOOTSTRAP_PASSWORD are required")
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	var userID string
	if err := pool.QueryRow(ctx, "INSERT INTO users (email, password_hash) VALUES (lower($1), $2) RETURNING id", email, hash).Scan(&userID); err != nil {
		return fmt.Errorf("create bootstrap user: %w", err)
	}
	fmt.Printf("user_id=%s\nemail=%s\n", userID, email)
	return nil
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	pool, err := db.Open(connectCtx, cfg.DatabaseURL)
	if err != nil {
		cancelConnect()
		return err
	}
	if err := db.Migrate(connectCtx, pool); err != nil {
		cancelConnect()
		pool.Close()
		return fmt.Errorf("migrate database: %w", err)
	}
	providers, err := configureStorage(connectCtx, cfg, pool)
	if err != nil {
		cancelConnect()
		pool.Close()
		return err
	}
	cancelConnect()
	defer pool.Close()
	defer providers.Close()
	logger.Info("storage providers configured", "providers", providers.Names())
	uploadService, err := upload.NewService(
		upload.NewRepository(pool), chunk.NewRepository(pool), account.NewRepository(pool),
		providers, placement.NewDefault(), upload.ServiceConfig{
			ChunkSize: cfg.ChunkSizeBytes, SessionLifetime: cfg.UploadSessionLifetime,
			GlobalUploadLimit: cfg.GlobalUploadLimit,
		}, filedomain.NewRepository(pool),
	)
	if err != nil {
		return fmt.Errorf("configure upload service: %w", err)
	}
	uploadHandler := upload.NewHandler(uploadService, cfg.APIUserID, job.NewRepository(pool))
	downloadService, err := download.NewService(
		filedomain.NewRepository(pool), chunk.NewRepository(pool), account.NewRepository(pool), providers,
	)
	if err != nil {
		return fmt.Errorf("configure download service: %w", err)
	}
	downloadHandler := download.NewHandler(downloadService, cfg.APIUserID)
	fileHandler := filedomain.NewHandler(filedomain.NewRepository(pool), cfg.APIUserID, func(ctx context.Context, userID, typ string, payload any, maxAttempts int) (string, error) {
		return job.NewRepository(pool).Enqueue(ctx, userID, job.Type(typ), payload, maxAttempts)
	})
	directoryHandler := directory.NewHandler(directory.NewRepository(pool), cfg.APIUserID)
	authHandler := auth.NewHandler(auth.NewUserRepository(pool), auth.NewSessionRepository(pool), 24*time.Hour, auth.NewAttemptRepository(pool))
	authHandler.SetCookieSecure(cfg.CookieSecure)

	server := &http.Server{
		Addr:              cfg.HTTPAddress,
		Handler:           httpapi.NewRouter(health.NewHandler(pool, cfg.HealthTimeout), uploadHandler, downloadHandler, fileHandler, directoryHandler, authHandler, account.NewHandler(account.NewRepository(pool), cfg.APIUserID, providerAccountRefresher{providers: providers})),
		ReadHeaderTimeout: cfg.HealthTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("api listening", "address", cfg.HTTPAddress)
		serverErrors <- server.ListenAndServe()
	}()

	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-signalCtx.Done():
		logger.Info("shutting down api")
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}
	return nil
}

func configureStorage(ctx context.Context, cfg config.Config, pool *pgxpool.Pool) (*storage.Registry, error) {
	localProvider, err := localstorage.New(cfg.LocalStorageRoot)
	if err != nil {
		return nil, fmt.Errorf("configure local provider: %w", err)
	}
	providers := storage.NewRegistry()
	if err := providers.Register("local", localProvider); err != nil {
		return nil, fmt.Errorf("register local provider: %w", err)
	}
	if cfg.ProtonAdapterAddress != "" {
		protonProvider, err := grpcstorage.New(cfg.ProtonAdapterAddress)
		if err != nil {
			return nil, fmt.Errorf("configure proton adapter: %w", err)
		}
		if err := providers.Register("proton", protonProvider); err != nil {
			_ = protonProvider.Close()
			return nil, fmt.Errorf("register proton provider: %w", err)
		}
	}
	if cfg.LocalAccountUserID == "" {
		return providers, nil
	}

	accounts, err := account.NewRepository(pool).ProvisionLocal(ctx, account.ProvisionLocalParams{
		UserID:             cfg.LocalAccountUserID,
		Count:              cfg.LocalAccountCount,
		TotalBytes:         cfg.LocalAccountQuotaBytes,
		MaxUploadWorkers:   cfg.LocalMaxUploadWorkers,
		MaxDownloadWorkers: cfg.LocalMaxDownloadWorkers,
	})
	if err != nil {
		return nil, fmt.Errorf("provision local accounts: %w", err)
	}
	for _, storageAccount := range accounts {
		if err := localProvider.Health(ctx, storageAccount); err != nil {
			return nil, fmt.Errorf("initialize local account %s: %w", storageAccount.Name, err)
		}
	}
	return providers, nil
}

func runHealthcheck() error {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:8080/health/ready", nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("readiness returned %s", response.Status)
	}
	return nil
}
