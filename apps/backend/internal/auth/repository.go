package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct{ ID, Email, PasswordHash string }
type UserRepository struct{ pool *pgxpool.Pool }

func NewUserRepository(pool *pgxpool.Pool) *UserRepository { return &UserRepository{pool: pool} }
func (r *UserRepository) GetByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := r.pool.QueryRow(ctx, "SELECT id, email, password_hash FROM users WHERE lower(email) = lower($1)", strings.TrimSpace(email)).Scan(&user.ID, &user.Email, &user.PasswordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, fmt.Errorf("user not found: %w", pgx.ErrNoRows)
	}
	if err != nil {
		return User{}, fmt.Errorf("get user by email: %w", err)
	}
	return user, nil
}
