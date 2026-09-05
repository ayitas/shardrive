package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const sessionTokenBytes = 32

type Session struct {
	ID, UserID            string
	ExpiresAt, LastSeenAt time.Time
}
type SessionRepository struct{ pool *pgxpool.Pool }

func NewSessionRepository(pool *pgxpool.Pool) *SessionRepository {
	return &SessionRepository{pool: pool}
}

func NewSessionToken() (string, []byte, error) {
	token := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(token); err != nil {
		return "", nil, fmt.Errorf("generate session token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(token)
	hash := sha256.Sum256([]byte(encoded))
	return encoded, hash[:], nil
}

func (r *SessionRepository) Create(ctx context.Context, userID string, tokenHash []byte, expiresAt time.Time) (Session, error) {
	var session Session
	err := r.pool.QueryRow(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at) VALUES ($1, $2, $3) RETURNING id, user_id, expires_at, last_seen_at`, userID, tokenHash, expiresAt).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &session.LastSeenAt)
	if err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return session, nil
}

func (r *SessionRepository) GetByTokenHash(ctx context.Context, tokenHash []byte, now time.Time) (Session, error) {
	var session Session
	err := r.pool.QueryRow(ctx, `UPDATE sessions SET last_seen_at = $2 WHERE token_hash = $1 AND expires_at > $2 RETURNING id, user_id, expires_at, last_seen_at`, tokenHash, now).Scan(&session.ID, &session.UserID, &session.ExpiresAt, &session.LastSeenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, fmt.Errorf("session not found: %w", pgx.ErrNoRows)
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	return session, nil
}

func (r *SessionRepository) Delete(ctx context.Context, tokenHash []byte) error {
	if _, err := r.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash = $1", tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func HashSessionToken(token string) []byte { hash := sha256.Sum256([]byte(token)); return hash[:] }
