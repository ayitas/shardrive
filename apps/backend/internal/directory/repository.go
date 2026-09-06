package directory

import (
	"context"
	"errors"
	"fmt"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, userID, name string, parentID *string) (Directory, error) {
	var value Directory
	var parentArg any
	if parentID != nil {
		parentArg = *parentID
	}
	err := r.pool.QueryRow(ctx, `
		INSERT INTO directories (user_id, parent_id, name)
		SELECT $1::uuid, $3::uuid, $2::text
		WHERE $3::uuid IS NULL OR EXISTS (
			SELECT 1 FROM directories WHERE id = $3::uuid AND user_id = $1::uuid
		)
		RETURNING id, user_id, parent_id, name, created_at, updated_at
	`, userID, name, parentArg).Scan(
		&value.ID, &value.UserID, &value.ParentID, &value.Name, &value.CreatedAt, &value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Directory{}, &domain.Error{Kind: domain.ErrNotFound, Op: "create", Entity: "parent directory", Err: err}
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Directory{}, &domain.Error{Kind: domain.ErrConflict, Op: "create", Entity: "directory", Err: err}
		}
		return Directory{}, fmt.Errorf("create directory: %w", err)
	}
	return value, nil
}

func (r *Repository) ListByUser(ctx context.Context, userID string, parentID *string) ([]Directory, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, parent_id, name, created_at, updated_at
		FROM directories
		WHERE user_id = $1 AND parent_id IS NOT DISTINCT FROM $2
		ORDER BY name ASC, id ASC
		LIMIT 1000
	`, userID, parentID)
	if err != nil {
		return nil, fmt.Errorf("list directories: %w", err)
	}
	defer rows.Close()
	result := make([]Directory, 0)
	for rows.Next() {
		var value Directory
		if err := rows.Scan(&value.ID, &value.UserID, &value.ParentID, &value.Name, &value.CreatedAt, &value.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan directory: %w", err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate directories: %w", err)
	}
	return result, nil
}

func (r *Repository) Rename(ctx context.Context, id, userID, name string) (Directory, error) {
	var value Directory
	err := r.pool.QueryRow(ctx, `
		UPDATE directories SET name = $3, updated_at = now()
		WHERE id = $1 AND user_id = $2
		RETURNING id, user_id, parent_id, name, created_at, updated_at
	`, id, userID, name).Scan(
		&value.ID, &value.UserID, &value.ParentID, &value.Name, &value.CreatedAt, &value.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Directory{}, &domain.Error{Kind: domain.ErrNotFound, Op: "rename", Entity: "directory", Err: err}
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Directory{}, &domain.Error{Kind: domain.ErrConflict, Op: "rename", Entity: "directory", Err: err}
		}
		return Directory{}, fmt.Errorf("rename directory: %w", err)
	}
	return value, nil
}

func (r *Repository) DeleteEmpty(ctx context.Context, id, userID string) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM directories AS d
		WHERE d.id = $1 AND d.user_id = $2
		  AND NOT EXISTS (SELECT 1 FROM directories AS child WHERE child.parent_id = d.id)
		  AND NOT EXISTS (SELECT 1 FROM files AS file WHERE file.directory_id = d.id AND file.deleted_at IS NULL)
	`, id, userID)
	if err != nil {
		return fmt.Errorf("delete directory: %w", err)
	}
	if result.RowsAffected() == 1 {
		return nil
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM directories WHERE id = $1 AND user_id = $2)`, id, userID).Scan(&exists); err != nil {
		return fmt.Errorf("check directory: %w", err)
	}
	if !exists {
		return &domain.Error{Kind: domain.ErrNotFound, Op: "delete", Entity: "directory"}
	}
	return &domain.Error{Kind: domain.ErrConflict, Op: "delete", Entity: "directory", Err: errors.New("directory is not empty")}
}
