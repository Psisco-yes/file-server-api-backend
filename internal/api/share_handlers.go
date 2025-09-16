package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"
	"strconv"

	"github.com/go-chi/chi/v5"
)

type ShareRequest struct {
	RecipientUsername string `json:"recipient_username"`
	Permissions       string `json:"permissions"`
}

func (s *Server) ShareNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	var req ShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Permissions != "read" && req.Permissions != "write" {
		http.Error(w, "Invalid permissions value. Must be 'read' or 'write'", http.StatusBadRequest)
		return
	}

	node, err := s.store.GetNodeByID(r.Context(), nodeID, claims.UserID)
	if err != nil {
		http.Error(w, "Internal server error while checking node ownership", http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.Error(w, "Node not found or you are not the owner", http.StatusNotFound)
		return
	}

	recipient, err := s.store.GetUserByUsername(r.Context(), req.RecipientUsername)
	if err != nil {
		http.Error(w, "Internal server error while finding recipient", http.StatusInternalServerError)
		return
	}
	if recipient == nil {
		http.Error(w, "Recipient user not found", http.StatusNotFound)
		return
	}

	if recipient.ID == claims.UserID {
		http.Error(w, "Cannot share a node with yourself", http.StatusBadRequest)
		return
	}

	params := database.ShareNodeParams{
		NodeID:      nodeID,
		SharerID:    claims.UserID,
		RecipientID: recipient.ID,
		Permissions: req.Permissions,
	}

	share, err := s.store.ShareNode(r.Context(), params)
	if err != nil {
		switch {
		case errors.Is(err, database.ErrShareAlreadyExists):
			http.Error(w, err.Error(), http.StatusConflict)
		default:
			log.Printf("ERROR: Failed to create share record: %v", err)
			http.Error(w, "Failed to share node", http.StatusInternalServerError)
		}
		return
	}

	payload := map[string]interface{}{
		"share_info": share,
		"node_info":  node,
	}

	s.store.LogEvent(r.Context(), recipient.ID, "node_shared_with_you", payload)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(share)
}

func (s *Server) ListSharingUsersHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	users, err := s.store.GetSharingUsers(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to retrieve list of sharing users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

func (s *Server) ListSharedNodesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	sharerUsername := r.URL.Query().Get("sharer_username")
	if sharerUsername == "" {
		http.Error(w, "sharer_username is required", http.StatusBadRequest)
		return
	}

	sharer, err := s.store.GetUserByUsername(r.Context(), sharerUsername)
	if err != nil || sharer == nil {
		http.Error(w, "Sharer not found", http.StatusNotFound)
		return
	}

	nodes, err := s.store.ListDirectlySharedNodes(r.Context(), claims.UserID, sharer.ID)
	if err != nil {
		http.Error(w, "Failed to list shared nodes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func (s *Server) ListOutgoingSharesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	shares, err := s.store.GetOutgoingShares(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "Failed to retrieve outgoing shares", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(shares)
}

func (s *Server) DeleteShareHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	shareIDStr := chi.URLParam(r, "shareId")
	shareID, err := strconv.ParseInt(shareIDStr, 10, 64)
	if err != nil {
		http.Error(w, "Invalid share ID format", http.StatusBadRequest)
		return
	}

	shareInfo, err := s.store.GetShareByID(r.Context(), shareID, claims.UserID)
	if err != nil {
		http.Error(w, "Failed to retrieve share information", http.StatusInternalServerError)
		return
	}
	if shareInfo == nil {
		http.Error(w, "Share not found or you do not have permission to delete it", http.StatusNotFound)
		return
	}

	if err := s.store.DeleteShare(r.Context(), shareID, claims.UserID); err != nil {
		http.Error(w, "Failed to delete share", http.StatusInternalServerError)
		return
	}

	payload := map[string]string{"node_id": shareInfo.NodeID}
	s.store.LogEvent(r.Context(), shareInfo.RecipientID, "share_revoked_for_you", payload)

	w.WriteHeader(http.StatusNoContent)
}
