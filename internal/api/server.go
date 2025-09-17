package api

import (
	"encoding/json"
	"log"
	"net/http"
	"serwer-plikow/internal/config"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/storage"
	"serwer-plikow/internal/websocket"
)

type Server struct {
	config  *config.Config
	store   *database.Store
	storage *storage.LocalStorage
	wsHub   *websocket.Hub
}

func NewServer(cfg *config.Config, store *database.Store, storage *storage.LocalStorage, wsHub *websocket.Hub) *Server {
	return &Server{
		config:  cfg,
		store:   store,
		storage: storage,
		wsHub:   wsHub,
	}
}

func (s *Server) HealthCheckHandler(w http.ResponseWriter, r *http.Request) {
	err := s.store.GetPool().Ping(r.Context())

	status := make(map[string]string)
	if err == nil {
		status["status"] = "ok"
		status["database"] = "connected"
		w.WriteHeader(http.StatusOK)
	} else {
		status["status"] = "error"
		status["database"] = "disconnected"
		log.Printf("Health check failed: database ping error: %v", err)
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}
