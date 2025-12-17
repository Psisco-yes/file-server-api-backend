package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// @Summary      Share a node
// @Description  Shares a file or folder with another user, granting them read or write permissions.
// @Tags         shares
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        nodeId       path      string        true  "Node ID to share"
// @Param        shareRequest body      ShareRequest  true  "Share details"
// @Success      201          {object}  ShareResponse
// @Failure      400          {string}  string "Bad Request - Invalid request or cannot share with oneself"
// @Failure      401          {string}  string "Unauthorized"
// @Failure      404          {string}  string "Not Found - Node or recipient not found"
// @Failure      409          {string}  string "Conflict - Node is already shared with this user"
// @Failure      429          {string}  string "Too Many Requests"
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

		payloadForRecipient := map[string]interface{}{"share_info": createdShare, "node_info": node}
		txErr = q.LogEvent(r.Context(), recipient.ID, "node_shared_with_you", payloadForRecipient)
		if txErr != nil {
			return txErr
		}

		payloadForSharer := map[string]interface{}{"share_info": createdShare, "node_info": node, "recipient_username": recipient.Username}
		txErr = q.LogEvent(r.Context(), claims.UserID, "node_share_created", payloadForSharer)

		return txErr
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

	payloadForRecipient := map[string]interface{}{"share_info": createdShare, "node_info": node}
	eventMsgRecipient := map[string]interface{}{"event_type": "node_shared_with_you", "payload": payloadForRecipient}
	eventBytesRecipient, _ := json.Marshal(eventMsgRecipient)
	s.wsHub.PublishEvent(recipient.ID, eventBytesRecipient)

	payloadForSharer := map[string]interface{}{"share_info": createdShare, "node_info": node, "recipient_username": recipient.Username}
	eventMsgSharer := map[string]interface{}{"event_type": "node_share_created", "payload": payloadForSharer}
	eventBytesSharer, _ := json.Marshal(eventMsgSharer)
	s.wsHub.PublishEvent(claims.UserID, eventBytesSharer)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createdShare)
}

// @Summary      List users who shared with me
// @Description  Gets a unique list of users who have shared one or more items with the currently authenticated user. This is the root level for the "Shared with me" view.
// @Tags         shares
// @Produce      json
// @Security     BearerAuth
// @Success      200  {array}   SharingUserResponse
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /shares/incoming/users [get]
func (s *Server) ListSharingUsersHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)

	users, err := s.store.GetSharingUsers(r.Context(), claims.UserID, limit, offset)
	if err != nil {
		http.Error(w, "Failed to retrieve list of sharing users", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(users)
}

// @Summary      List items shared by a user
// @Description  Lists files and folders shared with the current user by a specific sharer. Can list the root of shared items (by omitting parent_id) or the content of a shared subfolder (by providing its parent_id).
// @Tags         shares
// @Produce      json
// @Security     BearerAuth
// @Param        sharer_username  query     string  true   "Username of the person who shared the content"
// @Param        parent_id        query     string  false  "ID of the shared parent folder to list. Omit for the root of shared items."
// @Param        limit            query     int     false  "Number of items to return" default(100)
// @Param        offset           query     int     false  "Offset for pagination" default(0)
// @Param        sort             query     string  false  "Sort order. Comma-separated list of fields. Use '-' for descending. E.g., 'type,-name'"
// @Success      200              {array}   models.RichNode
// @Failure      400              {string}  string "Bad Request - Missing sharer_username"
// @Failure      401              {string}  string "Unauthorized"
// @Failure      404              {string}  string "Not Found - Sharer or folder not found, or access denied"
// @Failure      429              {string}  string "Too Many Requests"
// @Failure      500              {string}  string "Internal Server Error"
// @Router       /shares/incoming/nodes [get]
func (s *Server) ListSharedNodesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)
	sort := r.URL.Query().Get("sort")

	sharerUsername := r.URL.Query().Get("sharer_username")
	if sharerUsername == "" {
		http.Error(w, "sharer_username is required", http.StatusBadRequest)
		return
	}

	sharer, err := s.store.GetUserByUsername(r.Context(), sharerUsername)
	if err != nil {
		log.Printf("ERROR: Failed to find sharer '%s': %v", sharerUsername, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
	if sharer == nil {
		http.Error(w, "Sharer not found", http.StatusNotFound)
		return
	}

	parentIDStr := r.URL.Query().Get("parent_id")

	if parentIDStr == "" {
		nodes, err := s.store.ListRichDirectlySharedNodes(r.Context(), claims.UserID, sharer.ID, limit, offset, sort)
		if err != nil {
			log.Printf("ERROR: Failed to list rich directly shared nodes for user %d from sharer %d: %v", claims.UserID, sharer.ID, err)
			http.Error(w, "Failed to list shared nodes", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nodes)
		return
	}

	hasAccess, err := s.store.HasAccessToNode(r.Context(), parentIDStr, claims.UserID)
	if err != nil {
		log.Printf("ERROR: Failed to check access for user %d to node %s: %v", claims.UserID, parentIDStr, err)
		http.Error(w, "Failed to check access permissions", http.StatusInternalServerError)
		return
	}
	if !hasAccess {
		http.Error(w, "Shared folder not found or access denied", http.StatusNotFound)
		return
	}

	nodes, err := s.store.GetRichNodesByParentID(r.Context(), sharer.ID, claims.UserID, &parentIDStr, limit, offset, sort, "")
	if err != nil {
		log.Printf("ERROR: Failed to list children for shared node %s: %v", parentIDStr, err)
		http.Error(w, "Failed to list shared nodes content", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// @Summary      Revoke a share
// @Description  Revokes a share entry. Only the original sharer can do this.
// @Tags         shares
// @Security     BearerAuth
// @Param        shareId  path      int  true  "ID of the share to delete"
// @Success      204      {object}  nil    "No Content"
// @Failure      400      {string}  string "Bad Request - Invalid share ID"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      404      {string}  string "Not Found - Share not found or you are not the owner"
// @Failure      429      {string}  string "Too Many Requests"
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
		success, err := q.DeleteShare(r.Context(), shareID, claims.UserID)
		if err != nil {
			return err
		}

		if !success {
			return errors.New("failed to delete share, it might have been deleted already")
		}

		payloadForRecipient := map[string]string{"node_id": shareInfo.NodeID}
		err = q.LogEvent(r.Context(), shareInfo.RecipientID, "share_revoked_for_you", payloadForRecipient)
		if err != nil {
			return err
		}

		payloadForSharer := map[string]interface{}{"share_id": shareInfo.ID, "node_id": shareInfo.NodeID}
		err = q.LogEvent(r.Context(), claims.UserID, "node_share_revoked", payloadForSharer)

		return err
	})

	if txErr != nil {
		log.Printf("ERROR: Failed to delete share in transaction: %v", txErr)
		http.Error(w, "Failed to delete share", http.StatusInternalServerError)
		return
	}

	payloadForRecipient := map[string]string{"node_id": shareInfo.NodeID}
	eventMsgRecipient := map[string]interface{}{"event_type": "share_revoked_for_you", "payload": payloadForRecipient}
	eventBytesRecipient, _ := json.Marshal(eventMsgRecipient)
	s.wsHub.PublishEvent(shareInfo.RecipientID, eventBytesRecipient)

	payloadForSharer := map[string]interface{}{"share_id": shareInfo.ID, "node_id": shareInfo.NodeID}
	eventMsgSharer := map[string]interface{}{"event_type": "node_share_revoked", "payload": payloadForSharer}
	eventBytesSharer, _ := json.Marshal(eventMsgSharer)
	s.wsHub.PublishEvent(claims.UserID, eventBytesSharer)

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      List nodes I have shared
// @Description  Gets a paginated and sortable list of unique nodes (files and folders) that the currently authenticated user has shared with others.
// @Tags         shares
// @Produce      json
// @Security     BearerAuth
// @Param        limit      query     int     false  "Number of items to return" default(100)
// @Param        offset     query     int     false  "Offset for pagination" default(0)
// @Param        sort       query     string  false  "Sort order. Comma-separated list of fields. Use '-' for descending. E.g., 'type,-name'"
// @Success      200  {array}   models.RichNode
// @Failure      401  {string}  string "Unauthorized"
// @Failure      429  {string}  string "Too Many Requests"
// @Failure      500  {string}  string "Internal Server Error"
// @Router       /shares/outgoing/nodes [get]
func (s *Server) ListOutgoingSharedNodesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	limit, offset := parsePagination(r)
	sort := r.URL.Query().Get("sort")

	nodes, err := s.store.GetRichOutgoingSharedNodes(r.Context(), claims.UserID, limit, offset, sort)
	if err != nil {
		http.Error(w, "Failed to retrieve outgoing shared nodes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// @Summary      List writeable shared folders
// @Description  Retrieves a list of folders shared by a specific user to which you have write access. This is primarily used for building a folder tree in a "Move to..." or "Copy to..." dialog within a shared context. It allows navigation by providing a `parent_id`.
// @Tags         shares
// @Produce      json
// @Security     BearerAuth
// @Param        sharer_username  query     string  true   "Username of the person who owns the shared content"
// @Param        parent_id        query     string  false  "The ID of the parent shared folder to list. Omit for the root level of folders you have write-access to from this user."
// @Success      200              {array}   models.RichNode
// @Failure      400              {string}  string  "Bad Request - Missing 'sharer_username' parameter"
// @Failure      401              {string}  string  "Unauthorized"
// @Failure      403              {string}  string  "Forbidden - You have read-only access to the parent folder"
// @Failure      404              {string}  string  "Not Found - Sharer user not found, or parent folder not found/access denied"
// @Failure      429              {string}  string  "Too Many Requests"
// @Failure      500              {string}  string  "Internal Server Error"
// @Router       /shares/incoming/writeable-folders [get]
func (s *Server) ListWriteableSharedFoldersHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	sharerUsername := r.URL.Query().Get("sharer_username")
	if sharerUsername == "" {
		http.Error(w, "'sharer_username' query parameter is required", http.StatusBadRequest)
		return
	}

	sharer, err := s.store.GetUserByUsername(r.Context(), sharerUsername)
	if err != nil {
		http.Error(w, "Failed to find sharer user", http.StatusInternalServerError)
		return
	}
	if sharer == nil {
		http.Error(w, "Sharer user not found", http.StatusNotFound)
		return
	}

	parentIDStr := r.URL.Query().Get("parent_id")
	var parentID *string
	if parentIDStr != "" {
		hasWrite, err := s.store.CheckWritePermission(r.Context(), claims.UserID, &parentIDStr)
		if err != nil {
			http.Error(w, "Failed to verify parent folder permissions", http.StatusInternalServerError)
			return
		}
		if !hasWrite {
			parent, _ := s.store.GetNodeIfAccessible(r.Context(), parentIDStr, claims.UserID)
			if parent == nil {
				http.Error(w, "Parent folder not found or access denied", http.StatusNotFound)
				return
			}
			http.Error(w, "Access denied: you do not have write permission for the parent folder", http.StatusForbidden)
			return
		}
		parentID = &parentIDStr
	}

	nodes, err := s.store.GetRichWriteableSharedFolders(r.Context(), claims.UserID, sharer.ID, parentID)
	if err != nil {
		http.Error(w, "Failed to retrieve writeable shared folders", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
