package account

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

func (r *Repository) Create(ctx context.Context, p CreateParams) (Account, error) {
	var value Account
	err := scanAccount(r.pool.QueryRow(ctx, `
		INSERT INTO storage_accounts
			(user_id, name, provider, total_bytes, priority, max_upload_workers, max_download_workers, credential_ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, name, provider, status, total_bytes, used_bytes, free_bytes,
			priority, max_upload_workers, max_download_workers, credential_ref,
			rate_limited_until, last_health_check, last_error, created_at, updated_at
	`, p.UserID, p.Name, p.Provider, p.TotalBytes, p.Priority, p.MaxUploadWorkers,
		p.MaxDownloadWorkers, p.CredentialRef), &value)
	if err != nil {
		return Account{}, mapError("create", err)
	}
	return value, nil
}

func (r *Repository) Get(ctx context.Context, id string) (Account, error) {
	var value Account
	err := scanAccount(r.pool.QueryRow(ctx, `
		SELECT id, user_id, name, provider, status, total_bytes, used_bytes, free_bytes,
			priority, max_upload_workers, max_download_workers, credential_ref,
			rate_limited_until, last_health_check, last_error, created_at, updated_at
		FROM storage_accounts WHERE id = $1
	`, id), &value)
	if err != nil {
		return Account{}, mapError("get", err)
	}
	return value, nil
}

func (r *Repository) ListByUser(ctx context.Context, userID string) ([]Account, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, name, provider, status, total_bytes, used_bytes, free_bytes,
			priority, max_upload_workers, max_download_workers, credential_ref,
			rate_limited_until, last_health_check, last_error, created_at, updated_at
		FROM storage_accounts WHERE user_id = $1 ORDER BY name, id
	`, userID)
	if err != nil {
		return nil, mapError("list", err)
	}
	defer rows.Close()

	values := make([]Account, 0)
	for rows.Next() {
		var value Account
		if err := scanAccount(rows, &value); err != nil {
			return nil, mapError("list", err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError("list", err)
	}
	return values, nil
}

// ProvisionLocal ensures local-1 through local-N exist for an existing user.
// It never creates placeholder users or removes accounts when N is reduced.
func (r *Repository) ProvisionLocal(ctx context.Context, p ProvisionLocalParams) ([]Account, error) {
	if p.UserID == "" || p.Count <= 0 || p.TotalBytes <= 0 || p.MaxUploadWorkers <= 0 || p.MaxDownloadWorkers <= 0 {
		return nil, &domain.Error{Kind: domain.ErrInvalid, Op: "provision", Entity: "local storage accounts"}
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin provision local accounts: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	values := make([]Account, 0, p.Count)
	for index := 1; index <= p.Count; index++ {
		name := fmt.Sprintf("local-%d", index)
		var value Account
		err := scanAccount(tx.QueryRow(ctx, `
			INSERT INTO storage_accounts
				(user_id, name, provider, total_bytes, max_upload_workers, max_download_workers)
			VALUES ($1, $2, 'local', $3, $4, $5)
			ON CONFLICT (user_id, name) DO UPDATE SET
				total_bytes = EXCLUDED.total_bytes,
				max_upload_workers = EXCLUDED.max_upload_workers,
				max_download_workers = EXCLUDED.max_download_workers,
				updated_at = now()
			WHERE storage_accounts.provider = 'local'
				AND storage_accounts.used_bytes <= EXCLUDED.total_bytes
			RETURNING id, user_id, name, provider, status, total_bytes, used_bytes, free_bytes,
				priority, max_upload_workers, max_download_workers, credential_ref,
				rate_limited_until, last_health_check, last_error, created_at, updated_at
		`, p.UserID, name, p.TotalBytes, p.MaxUploadWorkers, p.MaxDownloadWorkers), &value)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &domain.Error{Kind: domain.ErrConflict, Op: "provision", Entity: "storage account " + name, Err: errors.New("existing account is not local or quota is below current usage")}
		}
		if err != nil {
			return nil, mapError("provision", err)
		}
		values = append(values, value)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit provision local accounts: %w", err)
	}
	return values, nil
}

type rowScanner interface{ Scan(...any) error }

func scanAccount(row rowScanner, value *Account) error {
	return row.Scan(&value.ID, &value.UserID, &value.Name, &value.Provider, &value.State,
		&value.TotalBytes, &value.UsedBytes, &value.FreeBytes, &value.Priority,
		&value.MaxUploadWorkers, &value.MaxDownloadWorkers, &value.CredentialRef,
		&value.RateLimitedUntil, &value.LastHealthCheck, &value.LastError,
		&value.CreatedAt, &value.UpdatedAt)
}

func mapError(op string, err error) error {
	kind := error(nil)
	if errors.Is(err, pgx.ErrNoRows) {
		kind = domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		kind = domain.ErrConflict
	}
	if kind == nil {
		return fmt.Errorf("%s storage account: %w", op, err)
	}
	return &domain.Error{Kind: kind, Op: op, Entity: "storage account", Err: err}
}
