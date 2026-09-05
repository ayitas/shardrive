package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LoginAttempt struct {
	Email           string
	FailedCount     int
	WindowStartedAt time.Time
	LockedUntil     *time.Time
}
type AttemptRepository struct{ pool *pgxpool.Pool }

func NewAttemptRepository(pool *pgxpool.Pool) *AttemptRepository {
	return &AttemptRepository{pool: pool}
}

func (r *AttemptRepository) RecordFailure(ctx context.Context, email string, window time.Duration, lock time.Duration, maxFailures int) (LoginAttempt, error) {
	if window <= 0 || lock <= 0 || maxFailures <= 0 {
		return LoginAttempt{}, fmt.Errorf("invalid login attempt policy")
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var value LoginAttempt
	err := r.pool.QueryRow(ctx, `
		INSERT INTO auth_login_attempts (email, failed_count, window_started_at, locked_until)
		VALUES ($1, 1, now(), CASE WHEN 1 >= $4 THEN now() + $3 ELSE NULL END)
		ON CONFLICT (email) DO UPDATE SET
			failed_count = CASE WHEN auth_login_attempts.window_started_at + $2 < now() THEN 1 ELSE auth_login_attempts.failed_count + 1 END,
			window_started_at = CASE WHEN auth_login_attempts.window_started_at + $2 < now() THEN now() ELSE auth_login_attempts.window_started_at END,
			locked_until = CASE WHEN (CASE WHEN auth_login_attempts.window_started_at + $2 < now() THEN 1 ELSE auth_login_attempts.failed_count + 1 END) >= $4 THEN now() + $3 ELSE auth_login_attempts.locked_until END,
			updated_at = now()
		RETURNING email, failed_count, window_started_at, locked_until
	`, email, window, lock, maxFailures).Scan(&value.Email, &value.FailedCount, &value.WindowStartedAt, &value.LockedUntil)
	if err != nil {
		return LoginAttempt{}, fmt.Errorf("record login failure: %w", err)
	}
	return value, nil
}

func (r *AttemptRepository) Reset(ctx context.Context, email string) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM auth_login_attempts WHERE email = $1", strings.ToLower(strings.TrimSpace(email))); err != nil {
		return fmt.Errorf("reset login attempts: %w", err)
	}
	return nil
}

func (r *AttemptRepository) IsLocked(ctx context.Context, email string, now time.Time) (bool, error) {
	var locked bool
	err := r.pool.QueryRow(ctx, "SELECT COALESCE((SELECT locked_until > $2 FROM auth_login_attempts WHERE email = $1), false)", strings.ToLower(strings.TrimSpace(email)), now).Scan(&locked)
	if err != nil {
		return false, fmt.Errorf("check login lock: %w", err)
	}
	return locked, nil
}
