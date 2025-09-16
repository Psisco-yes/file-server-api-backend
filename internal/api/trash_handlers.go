package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"

	"github.com/go-chi/chi/v5"
)

// PurgeTrashHandler permanently deletes all items from the trash.
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

	deletedFileIDs, err := s.store.PurgeTrash(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to purge trash from database", http.StatusInternalServerError)
		return
	}

	for _, fileID := range deletedFileIDs {
		if err := s.storage.Delete(fileID); err != nil {
			log.Printf("WARN: Failed to delete file %s from storage during purge: %v", fileID, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

// RestoreNodeHandler restores a file or folder from the trash.
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
func (s *Server) ListTrashHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	nodes, err := s.store.ListTrash(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to list trash contents", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// ListTrashHandler lists all items currently in the user's trash.
// @Summary      List trash contents
// @Description  Retrieves a list of all files and folders currently in the user's trash.
// @Tags         trash
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   NodeResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /trash [get]
func (s *Server) RestoreNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	success, err := s.store.RestoreNode(r.Context(), nodeID, claims.UserID)
	if err != nil {
		if errors.Is(err, database.ErrDuplicateNodeName) {
			http.Error(w, "Cannot restore: a node with the same name already exists in the original location", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to restore node", http.StatusInternalServerError)
		return
	}

	if !success {
		http.Error(w, "Node not found in trash or you do not have permission to restore it", http.StatusNotFound)
		return
	}

	restoredNode, _ := s.store.GetNodeByID(r.Context(), nodeID, claims.UserID)
	s.store.LogEvent(r.Context(), claims.UserID, "node_restored", restoredNode)

	w.WriteHeader(http.StatusOK)
}
