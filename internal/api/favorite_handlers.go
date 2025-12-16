package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"
	_ "serwer-plikow/internal/models"

	"github.com/go-chi/chi/v5"
)

// @Summary      Add a node to favorites
// @Description  Marks a file or folder as a favorite for the current user.
// @Tags         favorites
// @Security     BearerAuth
// @Param        nodeId   path      string  true  "Node ID to add to favorites"
// @Success      204      {null}    nil     "No Content"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      404      {string}  string "Not Found - Node does not exist or user lacks access"
// @Failure      409      {string}  string "Conflict - Node is already in favorites"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId}/favorite [post]
func (s *Server) AddFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		err := q.AddFavorite(r.Context(), claims.UserID, nodeID)
		if err != nil {
			return err
		}
		payload := map[string]string{"node_id": nodeID}
		return q.LogEvent(r.Context(), claims.UserID, "favorite_added", payload)
	})

	if txErr != nil {
		switch {
		case errors.Is(txErr, database.ErrNodeNotFound):
			http.Error(w, "Node not found or you do not have permission to access it", http.StatusNotFound)
		case errors.Is(txErr, database.ErrFavoriteAlreadyExists):
			http.Error(w, txErr.Error(), http.StatusConflict)
		default:
			http.Error(w, "Failed to add to favorites", http.StatusInternalServerError)
		}
		return
	}

	richNode, err := s.store.GetRichNodeIfAccessible(r.Context(), nodeID, claims.UserID)
	if err != nil || richNode == nil {
		log.Printf("WARN: Could not retrieve rich node %s after adding to favorites: %v", nodeID, err)
	} else {
		eventMsg := map[string]interface{}{"event_type": "node_updated", "payload": richNode}
		eventBytes, _ := json.Marshal(eventMsg)
		s.wsHub.PublishEvent(claims.UserID, eventBytes)
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Remove a node from favorites
// @Description  Removes a file or folder from the current user's list of favorites.
// @Tags         favorites
// @Security     BearerAuth
// @Param        nodeId   path      string  true  "Node ID to remove from favorites"
// @Success      204      {null}    nil     "No Content"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId}/favorite [delete]
func (s *Server) RemoveFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		_, err := q.RemoveFavorite(r.Context(), claims.UserID, nodeID)
		if err != nil {
			return err
		}

		payload := map[string]string{"node_id": nodeID}
		return q.LogEvent(r.Context(), claims.UserID, "favorite_removed", payload)
	})

	if txErr != nil {
		http.Error(w, "Failed to remove from favorites", http.StatusInternalServerError)
		return
	}

	richNode, err := s.store.GetRichNodeIfAccessible(r.Context(), nodeID, claims.UserID)
	if err != nil || richNode == nil {
		log.Printf("WARN: Could not retrieve rich node %s after removing from favorites: %v", nodeID, err)
	} else {
		eventMsg := map[string]interface{}{"event_type": "node_updated", "payload": richNode}
		eventBytes, _ := json.Marshal(eventMsg)
		s.wsHub.PublishEvent(claims.UserID, eventBytes)
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      List favorite nodes
// @Description  Retrieves a list of all files and folders marked as favorite by the current user.
// @Tags         favorites
// @Produce      json
// @Security     BearerAuth
// @Param        limit      query     int     false  "Number of items to return" default(100)
// @Param        offset     query     int     false  "Offset for pagination" default(0)
// @Param        sort       query     string  false  "Sort order. Comma-separated list of fields. Use '-' for descending. E.g., 'type,-name'"
// @Success      200  {array}   models.RichNode
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /favorites [get]
func (s *Server) ListFavoritesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)
	sort := r.URL.Query().Get("sort")

	nodes, err := s.store.GetRichFavorites(r.Context(), claims.UserID, limit, offset, sort)
	if err != nil {
		http.Error(w, "Failed to list favorites", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
