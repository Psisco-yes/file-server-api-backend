package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"

	"github.com/go-chi/chi/v5"
)

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
