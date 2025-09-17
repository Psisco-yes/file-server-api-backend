package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"serwer-plikow/internal/database"

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

	payload := map[string]string{"node_id": nodeID}
	eventMsg := map[string]interface{}{"event_type": "favorite_added", "payload": payload}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(claims.UserID, eventBytes)

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

	payload := map[string]string{"node_id": nodeID}
	eventMsg := map[string]interface{}{"event_type": "favorite_removed", "payload": payload}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(claims.UserID, eventBytes)

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      List favorite nodes
// @Description  Retrieves a list of all files and folders marked as favorite by the current user.
// @Tags         favorites
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   NodeResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /favorites [get]
func (s *Server) ListFavoritesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)

	nodes, err := s.store.ListFavorites(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		http.Error(w, "Failed to list favorites", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
