package database

import (
	"context"
	"serwer-plikow/internal/auth"
	"testing"

	"github.com/stretchr/testify/require"
)

func createRandomUser(t *testing.T) {
	hashedPassword, err := auth.HashPassword("secretpassword")
	require.NoError(t, err)

	query := `INSERT INTO users (username, password_hash, display_name) VALUES ($1, $2, $3)`
	_, err = testStore.pool.Exec(context.Background(), query, "testuser", hashedPassword, "Test User")
	require.NoError(t, err)
}

func TestGetUserByUsername(t *testing.T) {
	createRandomUser(t)

	foundUser, err := testStore.GetUserByUsername(context.Background(), "testuser")

	require.NoError(t, err)
	require.NotNil(t, foundUser)

	require.Equal(t, "testuser", foundUser.Username)
	require.Equal(t, "Test User", foundUser.DisplayName)
	require.NotEmpty(t, foundUser.PasswordHash)

	nonExistentUser, err := testStore.GetUserByUsername(context.Background(), "nonexistent")
	require.NoError(t, err)
	require.Nil(t, nonExistentUser)
}
