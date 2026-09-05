package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMigrateFreshDatabase(t *testing.T) {
	databaseURL := os.Getenv("SHARDRIVE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SHARDRIVE_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer admin.Close()

	databaseName := fmt.Sprintf("shardrive_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	var testPool *pgxpool.Pool
	defer func() {
		if testPool != nil {
			testPool.Close()
		}
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop test database: %v", err)
		}
	}()

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	poolConfig.ConnConfig.Database = databaseName
	testPool, err = pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	if err := testPool.Ping(ctx); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if err := Migrate(ctx, testPool); err != nil {
		t.Fatalf("idempotent Migrate() error = %v", err)
	}

	for _, table := range []string{
		"users", "directories", "storage_accounts", "files",
		"upload_sessions", "chunks", "jobs", "sessions", "auth_login_attempts", "schema_migrations",
	} {
		var exists bool
		if err := testPool.QueryRow(ctx,
			"SELECT to_regclass('public.' || $1) IS NOT NULL", table,
		).Scan(&exists); err != nil {
			t.Fatalf("check table %s: %v", table, err)
		}
		if !exists {
			t.Errorf("table %s does not exist", table)
		}
	}

	var applied int
	if err := testPool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&applied); err != nil {
		t.Fatalf("count schema migrations: %v", err)
	}
	if applied != 3 {
		t.Errorf("applied migrations = %d, want 3", applied)
	}

	var uuidPrimaryKeys int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint c
		JOIN pg_class t ON t.oid = c.conrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN unnest(c.conkey) AS key(attnum) ON true
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key.attnum
		WHERE n.nspname = 'public'
		  AND t.relname = ANY($1)
		  AND c.contype = 'p'
		  AND a.atttypid = 'uuid'::regtype
	`, []string{"users", "directories", "storage_accounts", "files", "upload_sessions", "chunks", "jobs"}).Scan(&uuidPrimaryKeys); err != nil {
		t.Fatalf("count UUID primary keys: %v", err)
	}
	if uuidPrimaryKeys != 7 {
		t.Errorf("UUID primary keys = %d, want 7", uuidPrimaryKeys)
	}

	var foreignKeys int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_constraint c
		JOIN pg_namespace n ON n.oid = c.connamespace
		WHERE n.nspname = 'public' AND c.contype = 'f'
	`).Scan(&foreignKeys); err != nil {
		t.Fatalf("count foreign keys: %v", err)
	}
	if foreignKeys < 10 {
		t.Errorf("foreign keys = %d, want at least 10", foreignKeys)
	}

	var uniqueChunkConstraint bool
	if err := testPool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint c
			JOIN pg_class t ON t.oid = c.conrelid
			WHERE t.relname = 'chunks'
			  AND c.contype = 'u'
			  AND pg_get_constraintdef(c.oid) LIKE '%(file_id, chunk_index)%'
		)
	`).Scan(&uniqueChunkConstraint); err != nil {
		t.Fatalf("check unique chunk constraint: %v", err)
	}
	if !uniqueChunkConstraint {
		t.Error("UNIQUE(file_id, chunk_index) constraint does not exist")
	}

	for _, index := range []string{
		"chunks_file_order_idx", "storage_accounts_user_status_idx", "jobs_pending_idx",
	} {
		var exists bool
		if err := testPool.QueryRow(ctx, "SELECT to_regclass('public.' || $1) IS NOT NULL", index).Scan(&exists); err != nil {
			t.Fatalf("check index %s: %v", index, err)
		}
		if !exists {
			t.Errorf("index %s does not exist", index)
		}
	}
}
