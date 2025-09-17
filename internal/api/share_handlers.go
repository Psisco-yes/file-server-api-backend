package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

type ShareRequest struct {
	RecipientUsername string `json:"recipient_username"`
	Permissions       string `json:"permissions"`
}

type SharingUserResponse struct {
	ID          int64  `json:"id" example:"2"`
	Username    string `json:"username" example:"user2"`
	DisplayName string `json:"display_name" example:"Test User"`
}

type OutgoingShareResponse struct {
	ID                int64     `json:"id"`
	NodeID            string    `json:"node_id"`
	SharerID          int64     `json:"sharer_id"`
	RecipientID       int64     `json:"recipient_id"`
	Permissions       string    `json:"permissions"`
	SharedAt          time.Time `json:"shared_at"`
	NodeName          string    `json:"node_name" example:"Raport.docx"`
	NodeType          string    `json:"node_type" example:"file"`
	RecipientUsername string    `json:"recipient_username" example:"user2"`
}

type ShareResponse struct {
	ID          int64     `json:"id" example:"1"`
	NodeID      string    `json:"node_id" example:"_vx2a-43VqRT5wz_s9u4"`
	SharerID    int64     `json:"sharer_id" example:"1"`
	RecipientID int64     `json:"recipient_id" example:"2"`
	Permissions string    `json:"permissions" example:"read"`
	SharedAt    time.Time `json:"shared_at"`
}

// ShareNodeHandler handles sharing a node with another user.
// @Summary      Share a node
// @Description  Shares a file or folder with another user, granting them read or write permissions.
// @Tags         shares
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        nodeId       path      string        true  "Node ID to share"
// @Param        shareRequest body      ShareRequest  true  "Share details"
// @Success      201          {object}  ShareResponse
// @Failure      400          {string}  string "Bad Request"
// @Failure      401          {string}  string "Unauthorized"
// @Failure      404          {string}  string "Not Found - Node or recipient not found"
// @Failure      409          {string}  string "Conflict - Node is already shared with this user"
// @Failure      500          {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId}/share [post]
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

	var createdShare *models.Share

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		var txErr error
		createdShare, txErr = q.ShareNode(r.Context(), params)
		if txErr != nil {
			return txErr
		}

		payload := map[string]interface{}{
			"share_info": createdShare,
			"node_info":  node,
		}
		return q.LogEvent(r.Context(), recipient.ID, "node_shared_with_you", payload)
	})

	if txErr != nil {
		switch {
		case errors.Is(txErr, database.ErrShareAlreadyExists):
			http.Error(w, txErr.Error(), http.StatusConflict)
		case errors.Is(txErr, database.ErrRecipientNotFound):
			http.Error(w, "Recipient user not found", http.StatusNotFound)
		default:
			log.Printf("ERROR: Failed to create share record: %v", txErr)
			http.Error(w, "Failed to share node", http.StatusInternalServerError)
		}
		return
	}

	payload := map[string]interface{}{
		"share_info": createdShare,
		"node_info":  node,
	}
	eventMsg := map[string]interface{}{"event_type": "node_shared_with_you", "payload": payload}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(recipient.ID, eventBytes)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createdShare)
}

// ListSharingUsersHandler retrieves a list of users who have shared items with the current user.
// @Summary      List users who shared with me
// @Description  Gets a unique list of users who have shared one or more items with the currently authenticated user. This is the root level for the "Shared with me" view.
// @Tags         shares
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   SharingUserResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /shares/incoming/users [get]
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

// ListSharedNodesHandler lists the content shared by a specific user.
// @Summary      List items shared by a user
// @Description  Lists the files and folders directly shared with the current user by a specific sharer.
// @Tags         shares
// @Produce      json
// @Security     BearerAuth
// @Param        sharer_username  query     string  true  "Username of the person who shared the content"
// @Success      200              {array}   NodeResponse
// @Failure      400              {string}  string "Bad Request"
// @Failure      401              {string}  string "Unauthorized"
// @Failure      404              {string}  string "Not Found"
// @Failure      500              {string}  string "Internal Server Error"
// @Router       /shares/incoming/nodes [get]
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

// ListOutgoingSharesHandler retrieves a list of shares created by the current user.
// @Summary      List items I have shared
// @Description  Gets a list of all items the currently authenticated user has shared with others.
// @Tags         shares
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   OutgoingShareResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /shares/outgoing [get]
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

// DeleteShareHandler revokes a share.
// @Summary      Revoke a share
// @Description  Revokes a share entry. Only the original sharer can do this.
// @Tags         shares
// @Security     BearerAuth
// @Param        shareId  path      int  true  "ID of the share to delete"
// @Success      204      {null}    nil "No Content"
// @Failure      400      {string}  string "Bad Request"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      404      {string}  string "Not Found"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /shares/{shareId} [delete]
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

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		err := q.DeleteShare(r.Context(), shareID, claims.UserID)
		if err != nil {
			return err
		}

		payload := map[string]string{"node_id": shareInfo.NodeID}
		return q.LogEvent(r.Context(), shareInfo.RecipientID, "share_revoked_for_you", payload)
	})

	if txErr != nil {
		log.Printf("ERROR: Failed to delete share in transaction: %v", txErr)
		http.Error(w, "Failed to delete share", http.StatusInternalServerError)
		return
	}

	payload := map[string]string{"node_id": shareInfo.NodeID}
	eventMsg := map[string]interface{}{"event_type": "share_revoked_for_you", "payload": payload}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(shareInfo.RecipientID, eventBytes)

	w.WriteHeader(http.StatusNoContent)
}
