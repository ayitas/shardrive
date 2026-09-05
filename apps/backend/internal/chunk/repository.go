package chunk

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

// Store atomically creates the authoritative chunk mapping and advances upload
// progress. A concurrent duplicate returns the committed row with created=false.
func (r *Repository) Store(ctx context.Context, p StoreParams) (value Chunk, created bool, err error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Chunk{}, false, fmt.Errorf("begin store chunk: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	err = scanChunk(tx.QueryRow(ctx, `
		INSERT INTO chunks (file_id, chunk_index, size_bytes, checksum_sha256,
			storage_account_id, remote_object_id, remote_path, state)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'STORED')
		ON CONFLICT (file_id, chunk_index) DO NOTHING
		RETURNING id, file_id, chunk_index, size_bytes, checksum_sha256,
			storage_account_id, remote_object_id, remote_path, state, retry_count, created_at, updated_at
	`, p.FileID, p.Index, p.SizeBytes, p.ChecksumSHA256, p.StorageAccountID,
		p.RemoteObjectID, p.RemotePath), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		err = scanChunk(tx.QueryRow(ctx, `
			SELECT id, file_id, chunk_index, size_bytes, checksum_sha256,
				storage_account_id, remote_object_id, remote_path, state, retry_count, created_at, updated_at
			FROM chunks WHERE file_id = $1 AND chunk_index = $2
		`, p.FileID, p.Index), &value)
		if err != nil {
			return Chunk{}, false, fmt.Errorf("get duplicate chunk: %w", err)
		}
		if value.State != StateStored || value.SizeBytes != p.SizeBytes ||
			value.ChecksumSHA256 == nil || *value.ChecksumSHA256 != p.ChecksumSHA256 {
			return Chunk{}, false, &domain.Error{Kind: domain.ErrConflict, Op: "store", Entity: "chunk", Err: fmt.Errorf("index %d differs from committed chunk", p.Index)}
		}
	} else if err != nil {
		return Chunk{}, false, fmt.Errorf("insert chunk: %w", err)
	} else {
		created = true
		command, updateErr := tx.Exec(ctx, `
			UPDATE upload_sessions
			SET received_bytes = received_bytes + $3,
				completed_chunks = completed_chunks + 1,
				updated_at = now()
			WHERE id = $1 AND file_id = $2 AND state = 'UPLOADING'
				AND received_bytes + $3 <= expected_size
				AND completed_chunks + 1 <= expected_chunks
		`, p.UploadID, p.FileID, p.SizeBytes)
		if updateErr != nil {
			return Chunk{}, false, fmt.Errorf("advance upload progress: %w", updateErr)
		}
		if command.RowsAffected() != 1 {
			return Chunk{}, false, &domain.Error{Kind: domain.ErrInvalidState, Op: "store", Entity: "upload session"}
		}
		command, updateErr = tx.Exec(ctx, `
			UPDATE storage_accounts
			SET used_bytes = used_bytes + $2, updated_at = now()
			WHERE id = $1 AND used_bytes + $2 <= total_bytes
		`, p.StorageAccountID, p.SizeBytes)
		if updateErr != nil {
			return Chunk{}, false, fmt.Errorf("advance storage account usage: %w", updateErr)
		}
		if command.RowsAffected() != 1 {
			return Chunk{}, false, &domain.Error{Kind: domain.ErrConflict, Op: "store", Entity: "storage account capacity"}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Chunk{}, false, fmt.Errorf("commit store chunk: %w", err)
	}
	return value, created, nil
}

func (r *Repository) GetByIndex(ctx context.Context, fileID string, index int) (Chunk, error) {
	var value Chunk
	err := scanChunk(r.pool.QueryRow(ctx, `
		SELECT id, file_id, chunk_index, size_bytes, checksum_sha256,
			storage_account_id, remote_object_id, remote_path, state, retry_count, created_at, updated_at
		FROM chunks WHERE file_id = $1 AND chunk_index = $2
	`, fileID, index), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return Chunk{}, &domain.Error{Kind: domain.ErrNotFound, Op: "get", Entity: "chunk", Err: err}
	}
	if err != nil {
		return Chunk{}, fmt.Errorf("get chunk: %w", err)
	}
	return value, nil
}

func (r *Repository) ListByFile(ctx context.Context, fileID string) ([]Chunk, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, file_id, chunk_index, size_bytes, checksum_sha256,
			storage_account_id, remote_object_id, remote_path, state, retry_count, created_at, updated_at
		FROM chunks WHERE file_id = $1 ORDER BY chunk_index
	`, fileID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	defer rows.Close()
	values := make([]Chunk, 0)
	for rows.Next() {
		var value Chunk
		if err := scanChunk(rows, &value); err != nil {
			return nil, fmt.Errorf("list chunks: %w", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	return values, nil
}

// MarkDeleted finalizes remote deletion and quota release in one transaction.
func (r *Repository) MarkDeleted(ctx context.Context, id string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin mark chunk deleted: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var accountID string
	var size int64
	if err := tx.QueryRow(ctx, "UPDATE chunks SET state = 'DELETED', updated_at = now() WHERE id = $1 AND state IN ('STORED', 'DELETING') RETURNING storage_account_id, size_bytes", id).Scan(&accountID, &size); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &domain.Error{Kind: domain.ErrConflict, Op: "delete", Entity: "chunk", Err: err}
		}
		return fmt.Errorf("mark chunk deleted: %w", err)
	}
	if _, err := tx.Exec(ctx, "UPDATE storage_accounts SET used_bytes = used_bytes - $2, updated_at = now() WHERE id = $1 AND used_bytes >= $2", accountID, size); err != nil {
		return fmt.Errorf("release chunk quota: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit chunk deletion: %w", err)
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanChunk(row rowScanner, value *Chunk) error {
	return row.Scan(&value.ID, &value.FileID, &value.Index, &value.SizeBytes,
		&value.ChecksumSHA256, &value.StorageAccountID, &value.RemoteObjectID,
		&value.RemotePath, &value.State, &value.RetryCount, &value.CreatedAt, &value.UpdatedAt)
}
