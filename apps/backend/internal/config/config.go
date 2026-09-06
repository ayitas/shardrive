package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultHTTPAddress             = ":8080"
	defaultConnectTimeout          = 10 * time.Second
	defaultHealthTimeout           = 2 * time.Second
	defaultShutdownTimeout         = 10 * time.Second
	defaultLocalStorageRoot        = "./data/storage"
	defaultLocalAccountCount       = 5
	defaultLocalAccountQuota       = int64(1 << 30)
	defaultLocalMaxUploadWorkers   = 2
	defaultLocalMaxDownloadWorkers = 1
	maxLocalAccountCount           = 1000
	defaultChunkSize               = int64(32 * 1024 * 1024)
	defaultUploadSessionLifetime   = 24 * time.Hour
	defaultGlobalUploadLimit       = 6
)

type Config struct {
	HTTPAddress             string
	DatabaseURL             string
	ConnectTimeout          time.Duration
	HealthTimeout           time.Duration
	ShutdownTimeout         time.Duration
	LocalStorageRoot        string
	ProtonAdapterAddress    string
	LocalAccountUserID      string
	LocalAccountCount       int
	LocalAccountQuotaBytes  int64
	LocalMaxUploadWorkers   int
	LocalMaxDownloadWorkers int
	ChunkSizeBytes          int64
	UploadSessionLifetime   time.Duration
	GlobalUploadLimit       int
	CookieSecure            bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddress:             envOrDefault("SHARDRIVE_HTTP_ADDRESS", defaultHTTPAddress),
		DatabaseURL:             os.Getenv("SHARDRIVE_DATABASE_URL"),
		ConnectTimeout:          defaultConnectTimeout,
		HealthTimeout:           defaultHealthTimeout,
		ShutdownTimeout:         defaultShutdownTimeout,
		LocalStorageRoot:        envOrDefault("SHARDRIVE_LOCAL_STORAGE_ROOT", defaultLocalStorageRoot),
		ProtonAdapterAddress:    os.Getenv("SHARDRIVE_PROTON_ADAPTER_ADDRESS"),
		LocalAccountUserID:      os.Getenv("SHARDRIVE_LOCAL_ACCOUNT_USER_ID"),
		LocalAccountCount:       defaultLocalAccountCount,
		LocalAccountQuotaBytes:  defaultLocalAccountQuota,
		LocalMaxUploadWorkers:   defaultLocalMaxUploadWorkers,
		LocalMaxDownloadWorkers: defaultLocalMaxDownloadWorkers,
		ChunkSizeBytes:          defaultChunkSize,
		UploadSessionLifetime:   defaultUploadSessionLifetime,
		GlobalUploadLimit:       defaultGlobalUploadLimit,
		CookieSecure:            true,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("SHARDRIVE_DATABASE_URL is required")
	}

	var err error
	if cfg.ConnectTimeout, err = durationFromEnv("SHARDRIVE_DB_CONNECT_TIMEOUT", cfg.ConnectTimeout); err != nil {
		return Config{}, err
	}
	if cfg.HealthTimeout, err = durationFromEnv("SHARDRIVE_DB_HEALTH_TIMEOUT", cfg.HealthTimeout); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationFromEnv("SHARDRIVE_SHUTDOWN_TIMEOUT", cfg.ShutdownTimeout); err != nil {
		return Config{}, err
	}
	if cfg.LocalAccountCount, err = positiveIntFromEnv("SHARDRIVE_LOCAL_ACCOUNT_COUNT", cfg.LocalAccountCount); err != nil {
		return Config{}, err
	}
	if cfg.LocalAccountQuotaBytes, err = positiveInt64FromEnv("SHARDRIVE_LOCAL_ACCOUNT_QUOTA_BYTES", cfg.LocalAccountQuotaBytes); err != nil {
		return Config{}, err
	}
	if cfg.LocalMaxUploadWorkers, err = positiveIntFromEnv("SHARDRIVE_LOCAL_MAX_UPLOAD_WORKERS", cfg.LocalMaxUploadWorkers); err != nil {
		return Config{}, err
	}
	if cfg.LocalMaxDownloadWorkers, err = positiveIntFromEnv("SHARDRIVE_LOCAL_MAX_DOWNLOAD_WORKERS", cfg.LocalMaxDownloadWorkers); err != nil {
		return Config{}, err
	}
	if cfg.LocalAccountCount > maxLocalAccountCount {
		return Config{}, fmt.Errorf("SHARDRIVE_LOCAL_ACCOUNT_COUNT must not exceed %d", maxLocalAccountCount)
	}
	if cfg.ChunkSizeBytes, err = positiveInt64FromEnv("SHARDRIVE_CHUNK_SIZE_BYTES", cfg.ChunkSizeBytes); err != nil {
		return Config{}, err
	}
	if cfg.UploadSessionLifetime, err = durationFromEnv("SHARDRIVE_UPLOAD_SESSION_LIFETIME", cfg.UploadSessionLifetime); err != nil {
		return Config{}, err
	}
	if cfg.GlobalUploadLimit, err = positiveIntFromEnv("SHARDRIVE_GLOBAL_UPLOAD_LIMIT", cfg.GlobalUploadLimit); err != nil {
		return Config{}, err
	}
	if raw := os.Getenv("SHARDRIVE_COOKIE_SECURE"); raw != "" {
		cfg.CookieSecure, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("SHARDRIVE_COOKIE_SECURE must be true or false")
		}
	}

	return cfg, nil
}

func positiveIntFromEnv(name string, fallback int) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func positiveInt64FromEnv(name string, fallback int64) (int64, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}

	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than zero", name)
	}
	return value, nil
}
