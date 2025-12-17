package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"
	"strings"

	"github.com/go-chi/chi/v5"
)

// @Summary      Purge trash
// @Description  Permanently deletes all files and folders from the user's trash. This action cannot be undone.
// @Tags         trash
// @Security     BearerAuth
// @Success      204  {object}  nil    "No Content"
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
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
// @Param        limit      query     int     false  "Number of items to return" default(100)
// @Param        offset     query     int     false  "Offset for pagination" default(0)
// @Param        sort       query     string  false  "Sort order. Comma-separated list of fields. Use '-' for descending. E.g., 'type,-name'"
// @Success      200  {array}   models.RichNode
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /trash [get]
func (s *Server) ListTrashHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)
	sort := r.URL.Query().Get("sort")

	nodes, err := s.store.GetRichTrash(r.Context(), claims.UserID, limit, offset, sort)
	if err != nil {
		http.Error(w, "Failed to list trash contents", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// @Summary      Restore a node from trash
// @Description  Restores a file or folder (and all of its contents) from the trash to its original location. Fails if a node with the same name already exists in the target location, unless 'renameOnConflict' is true.
// @Tags         nodes
// @Produce      json
// @Security     BearerAuth
// @Param        nodeId   path      string  true  "Node ID to restore"
// @Param        renameOnConflict query   boolean false "If true, renames the node (e.g., 'file (1).txt') on conflict instead of failing."
// @Success      200      {object}  models.RichNode
// @Failure      401      {string}  string "Unauthorized"
// @Failure      404      {string}  string "Not Found - Node not found in trash"
// @Failure      409      {string}  string "Conflict - a node with the same name already exists in the original location"
// @Failure      429      {string}  string "Too Many Requests"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId}/restore [post]
func (s *Server) RestoreNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	renameOnConflictStr := r.URL.Query().Get("renameOnConflict")
	renameOnConflict := strings.ToLower(renameOnConflictStr) == "true"

	var restoredIDs []string

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		var err error
		var rowsAffectedCount int64
		restoredIDs, rowsAffectedCount, err = q.RestoreNode(r.Context(), nodeID, claims.UserID, renameOnConflict)
		if err != nil {
			return err
		}
		if rowsAffectedCount == 0 {
			return database.ErrNodeNotFound
		}

		payload := map[string]interface{}{"restored_node_ids": restoredIDs}
		return q.LogEvent(r.Context(), claims.UserID, "nodes_restored", payload)
	})

	if txErr != nil {
		switch {
		case errors.Is(txErr, database.ErrNodeNotFound):
			http.Error(w, "Node not found in trash", http.StatusNotFound)
		case errors.Is(txErr, database.ErrDuplicateNodeName):
			http.Error(w, "Cannot restore: a node with the same name already exists in the original location", http.StatusConflict)
		default:
			log.Printf("ERROR: Failed to restore node in transaction: %v", txErr)
			http.Error(w, "Failed to restore node", http.StatusInternalServerError)
		}
		return
	}

	var restoredRichNodes []*models.RichNode
	for _, id := range restoredIDs {
		node, err := s.store.GetRichNodeIfAccessible(r.Context(), id, claims.UserID)
		if err == nil && node != nil {
			restoredRichNodes = append(restoredRichNodes, node)
		} else {
			log.Printf("WARN: Could not retrieve restored rich node %s: %v", id, err)
		}
	}

	if len(restoredRichNodes) == 0 {
		http.Error(w, "Failed to retrieve details of restored nodes", http.StatusInternalServerError)
		return
	}

	var rootRestoredNode *models.RichNode
	for _, rn := range restoredRichNodes {
		if rn.ID == nodeID {
			rootRestoredNode = rn
			break
		}
	}
	if rootRestoredNode == nil {
		http.Error(w, "Failed to retrieve primary restored node", http.StatusInternalServerError)
		return
	}

	eventMsg := map[string]interface{}{"event_type": "nodes_restored", "payload": restoredRichNodes}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(claims.UserID, eventBytes)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(rootRestoredNode)
}

// @Summary      Permanently delete a single item from trash
// @Description  Permanently deletes a single file or folder (and all its contents) from the user's trash. This action cannot be undone.
// @Tags         trash
// @Security     BearerAuth
// @Param        nodeId   path      string  true  "The ID of the node to permanently delete from trash"
// @Success      204      {object}  nil     "No Content"
// @Failure      401      {string}  string  "Unauthorized"
// @Failure      404      {string}  string  "Not Found - Node not found in trash"
// @Failure      429      {string}  string  "Too Many Requests"
// @Failure      500      {string}  string  "Internal Server Error"
// @Router       /trash/{nodeId} [delete]
func (s *Server) PurgeSingleNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	var totalNodesDeleted int
	var deletedFileIDs []string
	var totalSizeFreed int64

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		var err error
		totalNodesDeleted, deletedFileIDs, totalSizeFreed, err = q.PurgeSingleNode(r.Context(), claims.UserID, nodeID)
		if err != nil {
			return err
		}

		if totalSizeFreed > 0 {
			return q.UpdateUserStorage(r.Context(), claims.UserID, -totalSizeFreed)
		}

		return nil
	})

	if txErr != nil {
		http.Error(w, "Failed to permanently delete node", http.StatusInternalServerError)
		return
	}

	if totalNodesDeleted == 0 {
		http.Error(w, "Node not found in trash", http.StatusNotFound)
		return
	}

	for _, fileID := range deletedFileIDs {
		if err := s.storage.Delete(fileID); err != nil {
			log.Printf("WARN: Failed to delete file %s from storage during single-item purge: %v", fileID, err)
		}
	}

	payload := map[string]string{"id": nodeID}
	eventMsg := map[string]interface{}{"event_type": "node_purged", "payload": payload}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(claims.UserID, eventBytes)

	w.WriteHeader(http.StatusNoContent)
}
