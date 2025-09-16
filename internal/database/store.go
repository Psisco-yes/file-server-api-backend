package database

import (
	"serwer-plikow/internal/websocket"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
}

type PostgresStore struct {
	pool  *pgxpool.Pool
	wsHub *websocket.Hub
}

func NewStore(pool *pgxpool.Pool, wsHub *websocket.Hub) *PostgresStore {
	return &PostgresStore{
		pool:  pool,
		wsHub: wsHub,
	}
}

func (s *PostgresStore) GetPool() *pgxpool.Pool {
	return s.pool
}
