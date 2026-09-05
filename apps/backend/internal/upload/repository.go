package upload

import (
	"context"
	"errors"
	"fmt"

	"github.com/ayitas/shardrive/apps/backend/internal/domain"
	filedomain "github.com/ayitas/shardrive/apps/backend/internal/file"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) CreateWithFile(ctx context.Context, p CreateAggregateParams) (filedomain.File, Session, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return filedomain.File{}, Session{}, fmt.Errorf("begin create upload: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var storedFile filedomain.File
	err = tx.QueryRow(ctx, `
		INSERT INTO files (user_id, directory_id, name, mime_type, size_bytes, chunk_size, chunk_count)
		SELECT $1, $2, $3, $4, $5, $6, $7
		WHERE $2::uuid IS NULL OR EXISTS (
			SELECT 1 FROM directories WHERE id = $2 AND user_id = $1
		)
		RETURNING id, user_id, directory_id, name, mime_type, size_bytes, chunk_size,
			chunk_count, checksum_sha256, state, created_at, updated_at, deleted_at
	`, p.UserID, p.DirectoryID, p.Name, p.MIMEType, p.SizeBytes, p.ChunkSize, p.ChunkCount).Scan(
		&storedFile.ID, &storedFile.UserID, &storedFile.DirectoryID, &storedFile.Name,
		&storedFile.MIMEType, &storedFile.SizeBytes, &storedFile.ChunkSize,
		&storedFile.ChunkCount, &storedFile.ChecksumSHA256, &storedFile.State,
		&storedFile.CreatedAt, &storedFile.UpdatedAt, &storedFile.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return filedomain.File{}, Session{}, &domain.Error{Kind: domain.ErrNotFound, Op: "create", Entity: "upload directory", Err: err}
	}
	if err != nil {
		return filedomain.File{}, Session{}, fmt.Errorf("create upload file: %w", err)
	}

	var session Session
	err = scanSession(tx.QueryRow(ctx, `
		INSERT INTO upload_sessions
			(user_id, file_id, expected_size, expected_chunks, state, expires_at)
		VALUES ($1, $2, $3, $4, 'UPLOADING', $5)
		RETURNING id, user_id, file_id, expected_size, received_bytes, expected_chunks,
			completed_chunks, state, expires_at, created_at, updated_at
	`, p.UserID, storedFile.ID, p.SizeBytes, p.ChunkCount, p.ExpiresAt), &session)
	if err != nil {
		return filedomain.File{}, Session{}, fmt.Errorf("create upload session: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return filedomain.File{}, Session{}, fmt.Errorf("commit create upload: %w", err)
	}
	return storedFile, session, nil
}

func (r *Repository) Create(ctx context.Context, p CreateParams) (Session, error) {
	var value Session
	err := scanSession(r.pool.QueryRow(ctx, `
		INSERT INTO upload_sessions (user_id, file_id, expected_size, expected_chunks, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, file_id, expected_size, received_bytes, expected_chunks,
			completed_chunks, state, expires_at, created_at, updated_at
	`, p.UserID, p.FileID, p.ExpectedSize, p.ExpectedChunks, p.ExpiresAt), &value)
	if err != nil {
		return Session{}, fmt.Errorf("create upload session: %w", err)
	}
	return value, nil
}

func (r *Repository) Get(ctx context.Context, id string) (Session, error) {
	var value Session
	err := scanSession(r.pool.QueryRow(ctx, `
		SELECT id, user_id, file_id, expected_size, received_bytes, expected_chunks,
			completed_chunks, state, expires_at, created_at, updated_at
		FROM upload_sessions WHERE id = $1
	`, id), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, &domain.Error{Kind: domain.ErrNotFound, Op: "get", Entity: "upload session", Err: err}
	}
	if err != nil {
		return Session{}, fmt.Errorf("get upload session: %w", err)
	}
	return value, nil
}

func (r *Repository) GetForUser(ctx context.Context, id, userID string) (Session, error) {
	var value Session
	err := scanSession(r.pool.QueryRow(ctx, `
		SELECT id, user_id, file_id, expected_size, received_bytes, expected_chunks,
			completed_chunks, state, expires_at, created_at, updated_at
		FROM upload_sessions WHERE id = $1 AND user_id = $2
	`, id, userID), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, &domain.Error{Kind: domain.ErrNotFound, Op: "get", Entity: "upload session", Err: err}
	}
	if err != nil {
		return Session{}, fmt.Errorf("get upload session for user: %w", err)
	}
	return value, nil
}

func (r *Repository) Transition(ctx context.Context, id string, from, to State) (Session, error) {
	if err := ValidateTransition(from, to); err != nil {
		return Session{}, err
	}
	var value Session
	err := scanSession(r.pool.QueryRow(ctx, `
		UPDATE upload_sessions SET state = $3, updated_at = now()
		WHERE id = $1 AND state = $2
		RETURNING id, user_id, file_id, expected_size, received_bytes, expected_chunks,
			completed_chunks, state, expires_at, created_at, updated_at
	`, id, from, to), &value)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, &domain.Error{Kind: domain.ErrConflict, Op: "transition", Entity: "upload session", Err: err}
	}
	if err != nil {
		return Session{}, fmt.Errorf("transition upload session: %w", err)
	}
	return value, nil
}

func (r *Repository) ValidateCompletion(ctx context.Context, id, userID string) (CompletionSnapshot, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CompletionSnapshot{}, fmt.Errorf("begin validate upload completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snapshot, err := lockCompletion(ctx, tx, id, userID)
	if err != nil {
		return CompletionSnapshot{}, err
	}
	if snapshot.Session.State == StateCompleted && snapshot.File.State == filedomain.StateAvailable && snapshot.File.ChecksumSHA256 != nil {
		snapshot.AlreadyCompleted = true
		if err := tx.Commit(ctx); err != nil {
			return CompletionSnapshot{}, fmt.Errorf("commit validate completed upload: %w", err)
		}
		return snapshot, nil
	}
	if snapshot.Session.State != StateUploading || snapshot.File.State != filedomain.StateUploading {
		return CompletionSnapshot{}, &domain.Error{Kind: domain.ErrInvalidState, Op: "complete", Entity: "upload"}
	}
	if err := validateCompletionRows(ctx, tx, snapshot); err != nil {
		return CompletionSnapshot{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return CompletionSnapshot{}, fmt.Errorf("commit validate upload completion: %w", err)
	}
	return snapshot, nil
}

func (r *Repository) FinishCompletion(ctx context.Context, id, userID, checksum string) (CompletionResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return CompletionResult{}, fmt.Errorf("begin finish upload completion: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snapshot, err := lockCompletion(ctx, tx, id, userID)
	if err != nil {
		return CompletionResult{}, err
	}
	if snapshot.Session.State == StateCompleted && snapshot.File.State == filedomain.StateAvailable &&
		snapshot.File.ChecksumSHA256 != nil && *snapshot.File.ChecksumSHA256 == checksum {
		if err := tx.Commit(ctx); err != nil {
			return CompletionResult{}, fmt.Errorf("commit idempotent upload completion: %w", err)
		}
		return CompletionResult{FileID: snapshot.File.ID, State: snapshot.File.State, ChecksumSHA256: checksum}, nil
	}
	if snapshot.Session.State != StateUploading || snapshot.File.State != filedomain.StateUploading {
		return CompletionResult{}, &domain.Error{Kind: domain.ErrConflict, Op: "complete", Entity: "upload"}
	}
	if err := validateCompletionRows(ctx, tx, snapshot); err != nil {
		return CompletionResult{}, err
	}
	if command, err := tx.Exec(ctx, `UPDATE upload_sessions SET state = 'VERIFYING', updated_at = now() WHERE id = $1 AND state = 'UPLOADING'`, id); err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return CompletionResult{}, fmt.Errorf("start upload verification: %w", err)
		}
		return CompletionResult{}, &domain.Error{Kind: domain.ErrConflict, Op: "complete", Entity: "upload session"}
	}
	if command, err := tx.Exec(ctx, `UPDATE files SET state = 'VERIFYING', updated_at = now() WHERE id = $1 AND state = 'UPLOADING'`, snapshot.File.ID); err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return CompletionResult{}, fmt.Errorf("start file verification: %w", err)
		}
		return CompletionResult{}, &domain.Error{Kind: domain.ErrConflict, Op: "complete", Entity: "file"}
	}
	if command, err := tx.Exec(ctx, `UPDATE files SET state = 'AVAILABLE', checksum_sha256 = $2, updated_at = now() WHERE id = $1 AND state = 'VERIFYING'`, snapshot.File.ID, checksum); err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return CompletionResult{}, fmt.Errorf("finish file verification: %w", err)
		}
		return CompletionResult{}, &domain.Error{Kind: domain.ErrConflict, Op: "complete", Entity: "file"}
	}
	if command, err := tx.Exec(ctx, `UPDATE upload_sessions SET state = 'COMPLETED', updated_at = now() WHERE id = $1 AND state = 'VERIFYING'`, id); err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return CompletionResult{}, fmt.Errorf("finish upload verification: %w", err)
		}
		return CompletionResult{}, &domain.Error{Kind: domain.ErrConflict, Op: "complete", Entity: "upload session"}
	}
	if err := tx.Commit(ctx); err != nil {
		return CompletionResult{}, fmt.Errorf("commit upload completion: %w", err)
	}
	return CompletionResult{FileID: snapshot.File.ID, State: filedomain.StateAvailable, ChecksumSHA256: checksum}, nil
}

func (r *Repository) FailIntegrity(ctx context.Context, id, userID, chunkID string) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin fail upload integrity: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	snapshot, err := lockCompletion(ctx, tx, id, userID)
	if err != nil {
		return err
	}
	if snapshot.Session.State != StateUploading || snapshot.File.State != filedomain.StateUploading {
		return &domain.Error{Kind: domain.ErrConflict, Op: "fail", Entity: "upload integrity"}
	}
	command, err := tx.Exec(ctx, `
		UPDATE chunks SET state = 'CORRUPT', updated_at = now()
		WHERE id = $1 AND file_id = $2 AND state = 'STORED'
	`, chunkID, snapshot.File.ID)
	if err != nil {
		return fmt.Errorf("mark corrupt chunk: %w", err)
	}
	if command.RowsAffected() != 1 {
		return &domain.Error{Kind: domain.ErrConflict, Op: "fail", Entity: "upload chunk"}
	}
	if command, err = tx.Exec(ctx, `UPDATE files SET state = 'FAILED', updated_at = now() WHERE id = $1 AND state = 'UPLOADING'`, snapshot.File.ID); err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return fmt.Errorf("fail corrupt file: %w", err)
		}
		return &domain.Error{Kind: domain.ErrConflict, Op: "fail", Entity: "file integrity"}
	}
	if command, err = tx.Exec(ctx, `UPDATE upload_sessions SET state = 'FAILED', updated_at = now() WHERE id = $1 AND state = 'UPLOADING'`, id); err != nil || command.RowsAffected() != 1 {
		if err != nil {
			return fmt.Errorf("fail corrupt upload: %w", err)
		}
		return &domain.Error{Kind: domain.ErrConflict, Op: "fail", Entity: "upload integrity"}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit failed upload integrity: %w", err)
	}
	return nil
}

type completionQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func lockCompletion(ctx context.Context, query completionQuerier, id, userID string) (CompletionSnapshot, error) {
	var snapshot CompletionSnapshot
	err := query.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.file_id, s.expected_size, s.received_bytes,
			s.expected_chunks, s.completed_chunks, s.state, s.expires_at, s.created_at, s.updated_at,
			f.id, f.user_id, f.directory_id, f.name, f.mime_type, f.size_bytes,
			f.chunk_size, f.chunk_count, f.checksum_sha256, f.state, f.created_at, f.updated_at, f.deleted_at
		FROM upload_sessions s
		JOIN files f ON f.id = s.file_id AND f.user_id = s.user_id
		WHERE s.id = $1 AND s.user_id = $2
		FOR UPDATE OF s, f
	`, id, userID).Scan(
		&snapshot.Session.ID, &snapshot.Session.UserID, &snapshot.Session.FileID,
		&snapshot.Session.ExpectedSize, &snapshot.Session.ReceivedBytes,
		&snapshot.Session.ExpectedChunks, &snapshot.Session.CompletedChunks,
		&snapshot.Session.State, &snapshot.Session.ExpiresAt, &snapshot.Session.CreatedAt,
		&snapshot.Session.UpdatedAt, &snapshot.File.ID, &snapshot.File.UserID,
		&snapshot.File.DirectoryID, &snapshot.File.Name, &snapshot.File.MIMEType,
		&snapshot.File.SizeBytes, &snapshot.File.ChunkSize, &snapshot.File.ChunkCount,
		&snapshot.File.ChecksumSHA256, &snapshot.File.State, &snapshot.File.CreatedAt,
		&snapshot.File.UpdatedAt, &snapshot.File.DeletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CompletionSnapshot{}, &domain.Error{Kind: domain.ErrNotFound, Op: "complete", Entity: "upload", Err: err}
	}
	if err != nil {
		return CompletionSnapshot{}, fmt.Errorf("lock upload completion: %w", err)
	}
	return snapshot, nil
}

func validateCompletionRows(ctx context.Context, query completionQuerier, snapshot CompletionSnapshot) error {
	if snapshot.Session.ExpectedSize != snapshot.File.SizeBytes || snapshot.Session.ExpectedChunks != snapshot.File.ChunkCount ||
		snapshot.Session.ReceivedBytes != snapshot.Session.ExpectedSize || snapshot.Session.CompletedChunks != snapshot.Session.ExpectedChunks {
		return &domain.Error{Kind: domain.ErrConflict, Op: "complete", Entity: "upload", Err: errors.New("progress does not match expected file")}
	}
	var total, stored, outsideRange int
	var storedBytes int64
	err := query.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE state = 'STORED'),
			COALESCE(sum(size_bytes), 0), count(*) FILTER (WHERE chunk_index >= $2)
		FROM chunks WHERE file_id = $1
	`, snapshot.File.ID, snapshot.Session.ExpectedChunks).Scan(&total, &stored, &storedBytes, &outsideRange)
	if err != nil {
		return fmt.Errorf("validate upload chunks: %w", err)
	}
	if total != snapshot.Session.ExpectedChunks || stored != total || storedBytes != snapshot.Session.ExpectedSize || outsideRange != 0 {
		return &domain.Error{Kind: domain.ErrConflict, Op: "complete", Entity: "upload chunks", Err: errors.New("stored chunk set is incomplete")}
	}
	return nil
}

type rowScanner interface{ Scan(...any) error }

func scanSession(row rowScanner, value *Session) error {
	return row.Scan(&value.ID, &value.UserID, &value.FileID, &value.ExpectedSize,
		&value.ReceivedBytes, &value.ExpectedChunks, &value.CompletedChunks, &value.State,
		&value.ExpiresAt, &value.CreatedAt, &value.UpdatedAt)
}
