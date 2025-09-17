package auth

import (
	"serwer-plikow/internal/models"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	password := "mySecretPassword123"
	hash, err := HashPassword(password)

	require.NoError(t, err)
	require.NotEmpty(t, hash)
	require.NotEqual(t, password, hash)
}

func TestCheckPasswordHash(t *testing.T) {
	password := "mySecretPassword123"
	hash, err := HashPassword(password)
	require.NoError(t, err)

	match := CheckPasswordHash(password, hash)
	require.True(t, match, "Password should match the hash")

	wrongPassword := "wrongPassword"
	match = CheckPasswordHash(wrongPassword, hash)
	require.False(t, match, "Wrong password should not match the hash")
}

func TestGenerateAndVerifyJWT(t *testing.T) {
	secret := "my_super_secret_key_for_testing"
	user := &models.User{
		ID:       123,
		Username: "testuser",
	}

	tokenString, err := GenerateJWT(user, secret)
	require.NoError(t, err)
	require.NotEmpty(t, tokenString)

	claims, err := VerifyJWT(tokenString, secret)
	require.NoError(t, err)
	require.NotNil(t, claims)
	require.Equal(t, user.ID, claims.UserID)
	require.Equal(t, user.Username, claims.Username)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), claims.ExpiresAt.Time, 5*time.Second)

	_, err = VerifyJWT(tokenString, "wrong_secret")
	require.Error(t, err)
	require.ErrorIs(t, err, jwt.ErrSignatureInvalid)

	expirationTime := time.Now().Add(-1 * time.Minute)
	claimsExpired := &AppClaims{
		UserID:   user.ID,
		Username: user.Username,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expirationTime),
		},
	}
	tokenExpired := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsExpired)
	tokenStringExpired, err := tokenExpired.SignedString([]byte(secret))
	require.NoError(t, err)

	_, err = VerifyJWT(tokenStringExpired, secret)
	require.Error(t, err)
	require.ErrorIs(t, err, jwt.ErrTokenExpired)
}
