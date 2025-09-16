package api

import (
	"serwer-plikow/internal/config"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/storage"
	"serwer-plikow/internal/websocket"
)

type Server struct {
	config  *config.Config
	store   *database.PostgresStore
	storage *storage.LocalStorage
	wsHub   *websocket.Hub
}

func NewServer(cfg *config.Config, store *database.PostgresStore, storage *storage.LocalStorage, wsHub *websocket.Hub) *Server {
	return &Server{
		config:  cfg,
		store:   store,
		storage: storage,
		wsHub:   wsHub,
	}
}
