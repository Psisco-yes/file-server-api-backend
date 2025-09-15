package database

import "github.com/jackc/pgx/v5/pgxpool"

type Store interface {
}

type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{
		pool: pool,
	}
}
