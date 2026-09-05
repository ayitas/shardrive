package directory

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

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
