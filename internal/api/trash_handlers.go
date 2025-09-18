package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"

	"github.com/go-chi/chi/v5"
)

// @Summary      Purge trash
// @Description  Permanently deletes all files and folders from the user's trash. This action cannot be undone.
// @Tags         trash
// @Security     BearerAuth
// @Success      204  {null}    nil "No Content"
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /trash/purge [delete]
func (s *Server) PurgeTrashHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	var deletedFileIDs []string
	var totalSizeFreed int64

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		var err error
		deletedFileIDs, totalSizeFreed, err = q.PurgeTrash(r.Context(), claims.UserID)
		if err != nil {
			return err
		}

		if totalSizeFreed > 0 {
			return q.UpdateUserStorage(r.Context(), claims.UserID, -totalSizeFreed)
		}

		return nil
	})

	if txErr != nil {
		http.Error(w, "Failed to purge trash", http.StatusInternalServerError)
		return
	}

	for _, fileID := range deletedFileIDs {
		if err := s.storage.Delete(fileID); err != nil {
			log.Printf("WARN: Failed to delete file %s from storage during purge: %v", fileID, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      List trash contents
// @Description  Retrieves a list of all files and folders currently in the user's trash.
// @Tags         trash
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   NodeResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /trash [get]
func (s *Server) ListTrashHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)

	nodes, err := s.store.ListTrash(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		http.Error(w, "Failed to list trash contents", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// @Summary      Restore a node from trash
// @Description  Restores a file or folder from the trash to its original location. Fails if a node with the same name already exists in the target location.
// @Tags         nodes
// @Security     BearerAuth
// @Param        nodeId   path      string  true  "Node ID to restore"
// @Success      200      {null}    nil   "OK"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      404      {string}  string "Not Found"
// @Failure      409      {string}  string "Conflict - a node with the same name already exists in the original location"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId}/restore [post]
func (s *Server) RestoreNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	var restoredNode *models.Node

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		success, err := q.RestoreNode(r.Context(), nodeID, claims.UserID)
		if err != nil {
			return err
		}
		if !success {
			return database.ErrNodeNotFound
		}

		restoredNode, err = q.GetNodeByID(r.Context(), nodeID, claims.UserID)
		if err != nil {
			return err
		}
		if restoredNode == nil {
			return errors.New("failed to retrieve restored node")
		}

		return q.LogEvent(r.Context(), claims.UserID, "node_restored", restoredNode)
	})

	if txErr != nil {
		if errors.Is(txErr, database.ErrNodeNotFound) {
			http.Error(w, "Node not found in trash...", http.StatusNotFound)
			return
		}
		if errors.Is(txErr, database.ErrDuplicateNodeName) {
			http.Error(w, "Cannot restore: a node with the same name already exists...", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to restore node", http.StatusInternalServerError)
		return
	}

	eventMsg := map[string]interface{}{"event_type": "node_restored", "payload": restoredNode}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(claims.UserID, eventBytes)

	w.WriteHeader(http.StatusOK)
}
