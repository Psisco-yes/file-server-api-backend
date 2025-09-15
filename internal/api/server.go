package api

import (
	"serwer-plikow/internal/config"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/storage"
)

type Server struct {
	config  *config.Config
	store   *database.PostgresStore
	storage *storage.LocalStorage
}

func NewServer(cfg *config.Config, store *database.PostgresStore, storage *storage.LocalStorage) *Server {
	return &Server{
		config:  cfg,
		store:   store,
		storage: storage,
	}
}
