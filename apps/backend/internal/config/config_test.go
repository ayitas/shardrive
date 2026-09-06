package config

import (
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	t.Setenv("SHARDRIVE_DATABASE_URL", "postgres://example")
	t.Setenv("SHARDRIVE_HTTP_ADDRESS", "127.0.0.1:9000")
	t.Setenv("SHARDRIVE_DB_CONNECT_TIMEOUT", "3s")
	t.Setenv("SHARDRIVE_DB_HEALTH_TIMEOUT", "500ms")
	t.Setenv("SHARDRIVE_SHUTDOWN_TIMEOUT", "4s")
	t.Setenv("SHARDRIVE_LOCAL_STORAGE_ROOT", "/tmp/shardrive-test-storage")
	t.Setenv("SHARDRIVE_API_USER_ID", "00000000-0000-0000-0000-000000000002")
	t.Setenv("SHARDRIVE_LOCAL_ACCOUNT_USER_ID", "00000000-0000-0000-0000-000000000001")
	t.Setenv("SHARDRIVE_LOCAL_ACCOUNT_COUNT", "7")
	t.Setenv("SHARDRIVE_LOCAL_ACCOUNT_QUOTA_BYTES", "2048")
	t.Setenv("SHARDRIVE_LOCAL_MAX_UPLOAD_WORKERS", "4")
	t.Setenv("SHARDRIVE_LOCAL_MAX_DOWNLOAD_WORKERS", "3")
	t.Setenv("SHARDRIVE_CHUNK_SIZE_BYTES", "1024")
	t.Setenv("SHARDRIVE_UPLOAD_SESSION_LIFETIME", "2h")
	t.Setenv("SHARDRIVE_GLOBAL_UPLOAD_LIMIT", "8")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddress != "127.0.0.1:9000" {
		t.Fatalf("HTTPAddress = %q", cfg.HTTPAddress)
	}
	if cfg.ConnectTimeout != 3*time.Second || cfg.HealthTimeout != 500*time.Millisecond || cfg.ShutdownTimeout != 4*time.Second {
		t.Fatalf("unexpected durations: %+v", cfg)
	}
	if cfg.LocalStorageRoot != "/tmp/shardrive-test-storage" || cfg.LocalAccountCount != 7 || cfg.LocalAccountQuotaBytes != 2048 || cfg.LocalMaxUploadWorkers != 4 || cfg.LocalMaxDownloadWorkers != 3 {
		t.Fatalf("unexpected local storage configuration: %+v", cfg)
	}
	if cfg.APIUserID != "00000000-0000-0000-0000-000000000002" {
		t.Fatalf("APIUserID = %q", cfg.APIUserID)
	}
	if cfg.ChunkSizeBytes != 1024 || cfg.UploadSessionLifetime != 2*time.Hour || cfg.GlobalUploadLimit != 8 {
		t.Fatalf("unexpected upload configuration: %+v", cfg)
	}
}

func TestLoadRejectsInvalidLocalAccountConfiguration(t *testing.T) {
	t.Setenv("SHARDRIVE_DATABASE_URL", "postgres://example")
	for _, variable := range []string{"SHARDRIVE_LOCAL_ACCOUNT_COUNT", "SHARDRIVE_LOCAL_ACCOUNT_QUOTA_BYTES", "SHARDRIVE_LOCAL_MAX_UPLOAD_WORKERS", "SHARDRIVE_LOCAL_MAX_DOWNLOAD_WORKERS", "SHARDRIVE_CHUNK_SIZE_BYTES", "SHARDRIVE_GLOBAL_UPLOAD_LIMIT"} {
		t.Run(variable, func(t *testing.T) {
			t.Setenv(variable, "0")
			if _, err := Load(); err == nil {
				t.Fatalf("Load() error = nil for %s", variable)
			}
		})
	}
}

func TestLoadRejectsExcessiveLocalAccountCount(t *testing.T) {
	t.Setenv("SHARDRIVE_DATABASE_URL", "postgres://example")
	t.Setenv("SHARDRIVE_LOCAL_ACCOUNT_COUNT", "1001")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil for excessive local account count")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("SHARDRIVE_DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want required database URL error")
	}
}

func TestLoadRejectsInvalidDuration(t *testing.T) {
	t.Setenv("SHARDRIVE_DATABASE_URL", "postgres://example")
	t.Setenv("SHARDRIVE_DB_HEALTH_TIMEOUT", "eventually")

	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want duration parsing error")
	}
}
