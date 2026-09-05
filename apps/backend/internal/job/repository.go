package job

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }
func (r *Repository) Enqueue(ctx context.Context, userID string, typ Type, payload any, maxAttempts int) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal job payload: %w", err)
	}
	var id string
	err = r.pool.QueryRow(ctx, "INSERT INTO jobs (user_id, type, payload, max_attempts) VALUES ($1, $2, $3, $4) RETURNING id", userID, typ, encoded, maxAttempts).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("enqueue job: %w", err)
	}
	return id, nil
}

func (r *Repository) Claim(ctx context.Context, workerID string) (Job, error) {
	var value Job
	err := r.pool.QueryRow(ctx, `WITH next AS (SELECT id FROM jobs WHERE state = 'PENDING' AND run_at <= now() ORDER BY run_at, created_at FOR UPDATE SKIP LOCKED LIMIT 1) UPDATE jobs j SET state = 'RUNNING', attempts = attempts + 1, locked_at = now(), locked_by = $1, updated_at = now() FROM next WHERE j.id = next.id RETURNING j.id, COALESCE(j.user_id::text, ''), j.type, j.state, j.payload::text, j.attempts, j.max_attempts`, workerID).Scan(&value.ID, &value.UserID, &value.Type, &value.State, &value.Payload, &value.Attempts, &value.MaxAttempts)
	if err != nil {
		return Job{}, fmt.Errorf("claim job: %w", err)
	}
	return value, nil
}

func (r *Repository) Complete(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, "UPDATE jobs SET state = 'COMPLETED', completed_at = now(), updated_at = now() WHERE id = $1 AND state = 'RUNNING'", id)
	if err != nil {
		return fmt.Errorf("complete job: %w", err)
	}
	return nil
}

func (r *Repository) Retry(ctx context.Context, id, message string) error {
	_, err := r.pool.Exec(ctx, "UPDATE jobs SET state = CASE WHEN attempts >= max_attempts THEN 'FAILED' ELSE 'PENDING' END, run_at = now() + interval '1 minute', last_error = $2, locked_at = NULL, locked_by = NULL, updated_at = now() WHERE id = $1 AND state = 'RUNNING'", id, message)
	if err != nil {
		return fmt.Errorf("retry job: %w", err)
	}
	return nil
}
