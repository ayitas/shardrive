package db

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ayitas/shardrive/apps/backend/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 6001156408456010102

type migration struct {
	version int64
	name    string
	sql     string
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	loaded, err := loadMigrations(migrations.Files)
	if err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version bigint PRIMARY KEY,
			name text NOT NULL,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedMigrations(ctx, conn)
	if err != nil {
		return err
	}

	for _, next := range loaded {
		if appliedName, ok := applied[next.version]; ok {
			if appliedName != next.name {
				return fmt.Errorf("migration %d name changed from %q to %q", next.version, appliedName, next.name)
			}
			continue
		}
		if err := applyMigration(ctx, conn, next); err != nil {
			return err
		}
	}

	return nil
}

func appliedMigrations(ctx context.Context, conn *pgxpool.Conn) (map[int64]string, error) {
	rows, err := conn.Query(ctx, "SELECT version, name FROM schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int64]string)
	for rows.Next() {
		var version int64
		var name string
		if err := rows.Scan(&version, &name); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, next migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %d: %w", next.version, err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if _, err := tx.Exec(ctx, next.sql, pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("apply migration %d (%s): %w", next.version, next.name, err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		next.version, next.name,
	); err != nil {
		return fmt.Errorf("record migration %d: %w", next.version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %d: %w", next.version, err)
	}
	return nil
}

func loadMigrations(source fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	var loaded []migration
	seen := make(map[int64]string)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".up.sql") {
			continue
		}
		prefix, name, ok := strings.Cut(strings.TrimSuffix(entry.Name(), ".up.sql"), "_")
		if !ok || prefix == "" || name == "" {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration version in %q", entry.Name())
		}
		if previous, exists := seen[version]; exists {
			return nil, fmt.Errorf("duplicate migration version %d in %q and %q", version, previous, entry.Name())
		}
		contents, err := fs.ReadFile(source, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		seen[version] = entry.Name()
		loaded = append(loaded, migration{version: version, name: name, sql: string(contents)})
	}

	sort.Slice(loaded, func(i, j int) bool { return loaded[i].version < loaded[j].version })
	return loaded, nil
}
