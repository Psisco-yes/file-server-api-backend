package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"serwer-plikow/internal/database"

	"github.com/go-chi/chi/v5"
)

func (s *Server) AddFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	err := s.store.AddFavorite(r.Context(), claims.UserID, nodeID)
	if err != nil {
		switch {
		case errors.Is(err, database.ErrNodeNotFound):
			http.Error(w, "Node not found or you do not have permission to access it", http.StatusNotFound)
		case errors.Is(err, database.ErrFavoriteAlreadyExists):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			http.Error(w, "Failed to add to favorites", http.StatusInternalServerError)
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) RemoveFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	success, err := s.store.RemoveFavorite(r.Context(), claims.UserID, nodeID)
	if err != nil {
		http.Error(w, "Failed to remove from favorites", http.StatusInternalServerError)
		return
	}

	if !success {

	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) ListFavoritesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	nodes, err := s.store.ListFavorites(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to list favorites", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
