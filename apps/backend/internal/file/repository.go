package file

import (
	"context"
	"errors"
	"fmt"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) Create(ctx context.Context, p CreateParams) (File, error) {
	var value File
	err := scanFile(r.pool.QueryRow(ctx, `
		INSERT INTO files (user_id, directory_id, name, mime_type, size_bytes, chunk_size, chunk_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, directory_id, name, mime_type, size_bytes, chunk_size,
			chunk_count, checksum_sha256, state, created_at, updated_at, deleted_at
	`, p.UserID, p.DirectoryID, p.Name, p.MIMEType, p.SizeBytes, p.ChunkSize, p.ChunkCount), &value)
	if err != nil {
		return File{}, fmt.Errorf("create file: %w", err)
	}
	return value, nil
}

func (r *Repository) Get(ctx context.Context, id string) (File, error) {
	var value File
	err := scanFile(r.pool.QueryRow(ctx, `
		SELECT id, user_id, directory_id, name, mime_type, size_bytes, chunk_size,
			chunk_count, checksum_sha256, state, created_at, updated_at, deleted_at
		FROM files WHERE id = $1
	`, id), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, &domain.Error{Kind: domain.ErrNotFound, Op: "get", Entity: "file", Err: err}
	}
	if err != nil {
		return File{}, fmt.Errorf("get file: %w", err)
	}
	return value, nil
}

func (r *Repository) GetForUser(ctx context.Context, id, userID string) (File, error) {
	var value File
	err := scanFile(r.pool.QueryRow(ctx, `
		SELECT id, user_id, directory_id, name, mime_type, size_bytes, chunk_size,
			chunk_count, checksum_sha256, state, created_at, updated_at, deleted_at
		FROM files WHERE id = $1 AND user_id = $2
	`, id, userID), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, &domain.Error{Kind: domain.ErrNotFound, Op: "get", Entity: "file", Err: err}
	}
	if err != nil {
		return File{}, fmt.Errorf("get file for user: %w", err)
	}
	return value, nil
}

func (r *Repository) ListByUser(ctx context.Context, userID string) ([]File, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, directory_id, name, mime_type, size_bytes, chunk_size,
			chunk_count, checksum_sha256, state, created_at, updated_at, deleted_at
		FROM files
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1000
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list files for user: %w", err)
	}
	defer rows.Close()
	values := make([]File, 0)
	for rows.Next() {
		var value File
		if err := rows.Scan(&value.ID, &value.UserID, &value.DirectoryID, &value.Name, &value.MIMEType,
			&value.SizeBytes, &value.ChunkSize, &value.ChunkCount, &value.ChecksumSHA256,
			&value.State, &value.CreatedAt, &value.UpdatedAt, &value.DeletedAt); err != nil {
			return nil, fmt.Errorf("scan listed file: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate listed files: %w", err)
	}
	return values, nil
}

func (r *Repository) Transition(ctx context.Context, id string, from, to State) (File, error) {
	if err := ValidateTransition(from, to); err != nil {
		return File{}, err
	}
	var value File
	err := scanFile(r.pool.QueryRow(ctx, `
		UPDATE files SET state = $3, updated_at = now(),
			deleted_at = CASE WHEN $3 = 'DELETED' THEN now() ELSE deleted_at END
		WHERE id = $1 AND state = $2
		RETURNING id, user_id, directory_id, name, mime_type, size_bytes, chunk_size,
			chunk_count, checksum_sha256, state, created_at, updated_at, deleted_at
	`, id, from, to), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return File{}, &domain.Error{Kind: domain.ErrConflict, Op: "transition", Entity: "file", Err: err}
	}
	if err != nil {
		return File{}, fmt.Errorf("transition file: %w", err)
	}
	return value, nil
}

type rowScanner interface{ Scan(...any) error }

func scanFile(row rowScanner, value *File) error {
	return row.Scan(&value.ID, &value.UserID, &value.DirectoryID, &value.Name, &value.MIMEType,
		&value.SizeBytes, &value.ChunkSize, &value.ChunkCount, &value.ChecksumSHA256,
		&value.State, &value.CreatedAt, &value.UpdatedAt, &value.DeletedAt)
}
