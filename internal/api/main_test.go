package api

import (
	"context"
	"log"
	"os"
	"serwer-plikow/internal/auth"
	"serwer-plikow/internal/config"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"
	"serwer-plikow/internal/storage"
	"serwer-plikow/internal/websocket"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var testServer *Server
var testUserToken string
var testUserClaims *auth.AppClaims

func TestMain(m *testing.M) {
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx,
		"postgres:14-alpine",
		postgres.WithDatabase("test_api_db"),
		postgres.WithUsername("user"),
		postgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	if err != nil {
		log.Fatalf("Could not start postgres: %s", err)
	}
	defer pgContainer.Terminate(ctx)

	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		log.Fatalf("Could not get connection string: %s", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		log.Fatalf("Could not connect to database: %s", err)
	}

	schema, err := os.ReadFile("../../db/init.sql")
	if err != nil {
		log.Fatalf("Could not read schema file: %s", err)
	}
	if _, err := pool.Exec(ctx, string(schema)); err != nil {
		log.Fatalf("Could not apply schema: %s", err)
	}

	tempDir, err := os.MkdirTemp("", "api-storage-test")
	if err != nil {
		log.Fatalf("Could not create temp dir: %s", err)
	}
	defer os.RemoveAll(tempDir)

	localStorage, err := storage.NewLocalStorage(tempDir)
	if err != nil {
		log.Fatalf("Could not create local storage: %s", err)
	}

	wsHub := websocket.NewHub()
	store := database.NewStore(pool, wsHub)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "api_test_secret"}}
	testServer = NewServer(cfg, store, localStorage, wsHub)

	hashedPassword, _ := auth.HashPassword("password")
	var userID int64
	var username = "api_test_user"
	pool.QueryRow(ctx, `INSERT INTO users (username, password_hash) VALUES ($1, $2) RETURNING id`, username, hashedPassword).Scan(&userID)

	testUser := &models.User{ID: userID, Username: username}
	testUserToken, err = auth.GenerateJWT(testUser, cfg.JWT.Secret)
	if err != nil {
		log.Fatalf("Could not generate token: %s", err)
	}

	testUserClaims, err = auth.VerifyJWT(testUserToken, cfg.JWT.Secret)
	if err != nil {
		log.Fatalf("Could not verify token: %s", err)
	}

	os.Exit(m.Run())
}
