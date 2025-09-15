package database

import (
	"context"
	"errors"
	"serwer-plikow/internal/models"

	"github.com/jackc/pgx/v5"
)

func (s *PostgresStore) GetUserByUsername(ctx context.Context, username string) (*models.User, error) {
	query := `
		SELECT 
			id, 
			username, 
			password_hash, 
			display_name, 
			created_at, 
			storage_quota_bytes, 
			storage_used_bytes
		FROM users
		WHERE username = $1
	`
	var user models.User

	err := s.pool.QueryRow(ctx, query, username).Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&user.DisplayName,
		&user.CreatedAt,
		&user.StorageQuotaBytes,
		&user.StorageUsedBytes,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &user, nil
}
