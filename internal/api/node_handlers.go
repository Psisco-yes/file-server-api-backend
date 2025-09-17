package api

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path"
	"serwer-plikow/internal/database"
	"serwer-plikow/internal/models"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jaevor/go-nanoid"
)

type CreateFolderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
}

type NodeResponse struct {
	ID         string    `json:"id" example:"_vx2a-43VqRT5wz_s9u4"`
	OwnerID    int64     `json:"owner_id" example:"1"`
	ParentID   *string   `json:"parent_id,omitempty" example:"fLW5kAh2ia9vYmjMnU4nZ"`
	Name       string    `json:"name" example:"Raport.docx"`
	NodeType   string    `json:"node_type" example:"file"`
	SizeBytes  *int64    `json:"size_bytes,omitempty" example:"1024"`
	MimeType   *string   `json:"mime_type,omitempty" example:"application/pdf"`
	CreatedAt  time.Time `json:"created_at"`
	ModifiedAt time.Time `json:"modified_at"`
}

func (s *Server) generateUniqueID(ctx context.Context) (string, error) {
	maxRetries := 10

	generateID, err := nanoid.Standard(21)
	if err != nil {
		return "", fmt.Errorf("failed to initialize nanoid generator: %w", err)
	}

	for i := 0; i < maxRetries; i++ {
		id := generateID()
		exists, err := s.store.NodeExists(ctx, id)
		if err != nil {
			return "", fmt.Errorf("failed to check for node existence: %w", err)
		}
		if !exists {
			return id, nil
		}
	}

	return "", fmt.Errorf("failed to generate a unique ID after %d attempts", maxRetries)
}

// CreateFolderHandler obsługuje tworzenie nowego folderu.
// @Summary      Create a new folder
// @Description  Creates a new folder in a specified parent folder or in the root directory if parent_id is omitted.
// @Tags         nodes
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        folderRequest  body      CreateFolderRequest  true  "Folder details"
// @Success      201            {object}  NodeResponse
// @Failure      400            {string}  string "Bad Request"
// @Failure      401            {string}  string "Unauthorized"
// @Failure      409            {string}  string "Conflict - folder with this name already exists"
// @Failure      500            {string}  string "Internal Server Error"
// @Router       /nodes/folder [post]
func (s *Server) CreateFolderHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	var req CreateFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		http.Error(w, "Folder name cannot be empty", http.StatusBadRequest)
		return
	}

	if req.ParentID != nil && len(*req.ParentID) != 21 {
		http.Error(w, "Invalid ParentID format", http.StatusBadRequest)
		return
	}

	nodeID, err := s.generateUniqueID(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	params := database.CreateNodeParams{
		ID:       nodeID,
		OwnerID:  claims.UserID,
		ParentID: req.ParentID,
		Name:     req.Name,
		NodeType: "folder",
	}

	var createdNode *models.Node

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		var txErr error
		createdNode, txErr = q.CreateNode(r.Context(), params)
		if txErr != nil {
			return txErr
		}

		return q.LogEvent(r.Context(), claims.UserID, "node_created", createdNode)
	})

	if txErr != nil {
		var pgErr *pgconn.PgError
		if errors.As(txErr, &pgErr) {
			switch pgErr.Code {
			case "23503": // foreign_key_violation
				http.Error(w, "Parent folder does not exist", http.StatusBadRequest)
				return
			case "23505": // unique_violation
				http.Error(w, "A folder with the same name already exists in this location", http.StatusConflict)
				return
			}
		}
		log.Printf("ERROR: Transaction failed in CreateFolderHandler: %v", txErr)
		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	eventMsg := map[string]interface{}{
		"event_type": "node_created",
		"payload":    createdNode,
	}
	eventBytes, err := json.Marshal(eventMsg)
	if err != nil {
		log.Printf("CRITICAL: Failed to marshal WebSocket event for node %s: %v", createdNode.ID, err)
	} else {
		s.wsHub.PublishEvent(claims.UserID, eventBytes)
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createdNode)
}

// ListNodesHandler obsługuje listowanie plików i folderów.
// @Summary      List nodes
// @Description  Lists files and folders. If 'shared_by_username' is provided, it lists shared content. Otherwise, it lists the user's own content.
// @Tags         nodes
// @Produce      json
// @Security     BearerAuth
// @Param        parent_id          query     string  false  "ID of the parent folder to list. Omit for root."
// @Param        shared_by_username query     string  false  "Username of the person who shared the content."
// @Success      200                {array}   NodeResponse
// @Failure      401                {string}  string "Unauthorized"
// @Failure      403                {string}  string "Forbidden"
// @Failure      404                {string}  string "Not Found"
// @Failure      500                {string}  string "Internal Server Error"
// @Router       /nodes [get]
func (s *Server) ListNodesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	parentIDStr := r.URL.Query().Get("parent_id")
	sharerUsername := r.URL.Query().Get("shared_by_username")

	if sharerUsername == "" {
		var parentID *string
		if parentIDStr != "" {
			parentID = &parentIDStr
		}

		nodes, err := s.store.GetNodesByParentID(r.Context(), claims.UserID, parentID)
		if err != nil {
			log.Printf("ERROR: Failed to list own nodes for user %d: %v", claims.UserID, err)
			http.Error(w, "Failed to list nodes", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nodes)
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

	if parentIDStr == "" {
		nodes, err := s.store.ListDirectlySharedNodes(r.Context(), claims.UserID, sharer.ID)
		if err != nil {
			log.Printf("ERROR: Failed to list directly shared nodes for user %d from sharer %d: %v", claims.UserID, sharer.ID, err)
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

	isOwner := sharer.ID == claims.UserID

	if !hasAccess && !isOwner {
		http.Error(w, "Shared folder not found or access denied", http.StatusNotFound)
		return
	}

	nodes, err := s.store.GetNodesByParentID(r.Context(), sharer.ID, &parentIDStr)
	if err != nil {
		log.Printf("ERROR: Failed to list children for shared node %s: %v", parentIDStr, err)
		http.Error(w, "Failed to list shared nodes content", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

// UploadFileHandler obsługuje wgrywanie jednego lub więcej plików.
// @Summary      Upload file(s)
// @Description  Uploads one or more files to a specified parent folder or the root directory. Use multipart/form-data.
// @Tags         nodes
// @Accept       multipart/form-data
// @Produce      json
// @Security     BearerAuth
// @Param        file       formData  file    true   "The file(s) to upload. Can be provided multiple times."
// @Param        parent_id  formData  string  false  "ID of the parent folder."
// @Success      201        {array}   NodeResponse
// @Failure      400        {string}  string "Bad Request"
// @Failure      401        {string}  string "Unauthorized"
// @Failure      500        {string}  string "Internal Server Error"
// @Router       /nodes/file [post]
func (s *Server) UploadFileHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, 1<<30) // 1GB limit na CAŁE zapytanie

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Error parsing multipart form", http.StatusBadRequest)
		return
	}

	parentIDStr := r.FormValue("parent_id")
	var parentID *string
	if parentIDStr != "" {
		if len(parentIDStr) != 21 {
			http.Error(w, "Invalid ParentID format", http.StatusBadRequest)
			return
		}
		parentID = &parentIDStr
	}

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(w, "No files uploaded", http.StatusBadRequest)
		return
	}

	var createdNodes []models.Node

	for _, handler := range files {
		file, err := handler.Open()
		if err != nil {
			log.Printf("ERROR opening multipart file %s: %v", handler.Filename, err)
			continue
		}
		defer file.Close()

		nodeID, err := s.generateUniqueID(r.Context())
		if err != nil {
			log.Printf("ERROR generating unique ID for file %s: %v", handler.Filename, err)
			continue
		}

		if err := s.storage.Save(nodeID, file); err != nil {
			log.Printf("ERROR saving file %s to storage: %v", handler.Filename, err)
			continue
		}

		sizeBytes := handler.Size
		mimeType := handler.Header.Get("Content-Type")
		params := database.CreateNodeParams{
			ID:        nodeID,
			OwnerID:   claims.UserID,
			ParentID:  parentID,
			Name:      handler.Filename,
			NodeType:  "file",
			SizeBytes: &sizeBytes,
			MimeType:  &mimeType,
		}

		var createdNode *models.Node

		txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
			var err error
			createdNode, err = q.CreateNode(r.Context(), params)
			if err != nil {
				return err
			}
			return q.LogEvent(r.Context(), claims.UserID, "node_created", createdNode)
		})

		if txErr != nil {
			log.Printf("ERROR creating db record for file %s: %v", handler.Filename, txErr)
			if cleanupErr := s.storage.Delete(nodeID); cleanupErr != nil {
				log.Printf("CRITICAL: Failed to clean up orphaned file %s: %v", nodeID, cleanupErr)
			}
			continue
		}

		eventMsg := map[string]interface{}{"event_type": "node_created", "payload": createdNode}
		eventBytes, _ := json.Marshal(eventMsg)
		s.wsHub.PublishEvent(claims.UserID, eventBytes)

		createdNodes = append(createdNodes, *createdNode)
	}

	if len(createdNodes) == 0 {
		http.Error(w, "None of the files could be processed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createdNodes)
}

// DownloadFileHandler obsługuje pobieranie pliku.
// @Summary      Download a file
// @Description  Downloads a single file by its ID.
// @Tags         nodes
// @Produce      application/octet-stream
// @Security     BearerAuth
// @Param        nodeId   path      string  true  "Node ID of the file to download"
// @Success      200      {file}    binary  "The file content"
// @Failure      400      {string}  string "Bad Request - Cannot download a folder"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      404      {string}  string "Not Found"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId}/download [get]
func (s *Server) DownloadFileHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	nodeID := chi.URLParam(r, "nodeId")
	if nodeID == "" {
		http.Error(w, "Node ID is required", http.StatusBadRequest)
		return
	}

	node, err := s.store.GetNodeIfAccessible(r.Context(), nodeID, claims.UserID)
	if err != nil {
		http.Error(w, "Failed to retrieve file metadata", http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.Error(w, "File not found or you do not have permission to access it", http.StatusNotFound)
		return
	}
	if node.NodeType != "file" {
		http.Error(w, "Cannot download a folder", http.StatusBadRequest)
		return
	}

	fileStream, err := s.storage.Get(node.ID)
	if err != nil {
		http.Error(w, "File not found on storage", http.StatusInternalServerError)
		return
	}
	defer fileStream.Close()

	w.Header().Set("Content-Disposition", "attachment; filename=\""+node.Name+"\"")
	if node.MimeType != nil && *node.MimeType != "" {
		w.Header().Set("Content-Type", *node.MimeType)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	if node.SizeBytes != nil {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", *node.SizeBytes))
	}

	io.Copy(w, fileStream)
}

// DeleteNodeHandler przenosi plik lub folder do kosza.
// @Summary      Move node to trash
// @Description  Moves a file or a folder (and its contents) to the trash (soft delete).
// @Tags         nodes
// @Security     BearerAuth
// @Param        nodeId   path      string  true  "Node ID to move to trash"
// @Success      204      {null}    nil     "No Content"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      404      {string}  string "Not Found"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId} [delete]
func (s *Server) DeleteNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	if nodeID == "" {
		http.Error(w, "Node ID is required", http.StatusBadRequest)
		return
	}

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		success, err := q.MoveNodeToTrash(r.Context(), nodeID, claims.UserID)
		if err != nil {
			return err
		}
		if !success {
			return database.ErrNodeNotFound
		}

		payload := map[string]string{"id": nodeID}
		return q.LogEvent(r.Context(), claims.UserID, "node_trashed", payload)
	})

	if txErr != nil {
		if errors.Is(txErr, database.ErrNodeNotFound) {
			http.Error(w, "Node not found or you do not have permission to delete it", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to delete node", http.StatusInternalServerError)
		return
	}

	payload := map[string]string{"id": nodeID}
	eventMsg := map[string]interface{}{"event_type": "node_trashed", "payload": payload}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(claims.UserID, eventBytes)

	w.WriteHeader(http.StatusNoContent)
}

type UpdateNodeRequest struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
}

// UpdateNodeHandler obsługuje zmianę nazwy lub przenoszenie pliku/folderu.
// @Summary      Update a node
// @Description  Updates a node's properties, such as its name or parent folder.
// @Tags         nodes
// @Accept       json
// @Security     BearerAuth
// @Param        nodeId         path      string             true  "Node ID to update"
// @Param        updateRequest  body      UpdateNodeRequest  true  "Properties to update"
// @Success      200            {null}    nil                "OK"
// @Failure      400            {string}  string "Bad Request"
// @Failure      401            {string}  string "Unauthorized"
// @Failure      404            {string}  string "Not Found"
// @Failure      409            {string}  string "Conflict"
// @Failure      500            {string}  string "Internal Server Error"
// @Router       /nodes/{nodeId} [patch]
func (s *Server) UpdateNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	originalNode, err := s.store.GetNodeByID(r.Context(), nodeID, claims.UserID)
	if err != nil {
		http.Error(w, "Failed to retrieve node", http.StatusInternalServerError)
		return
	}
	if originalNode == nil {
		http.Error(w, "Node not found or you do not have permission to modify it", http.StatusNotFound)
		return
	}

	var req UpdateNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	var updated bool

	if req.Name != nil {
		newName := strings.TrimSpace(*req.Name)
		if newName == "" {
			http.Error(w, "Name cannot be empty", http.StatusBadRequest)
			return
		}

		txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
			success, err := q.RenameNode(r.Context(), nodeID, claims.UserID, newName)
			if err != nil {
				return err
			}
			if !success {
				return database.ErrNodeNotFound
			}

			payload := map[string]interface{}{
				"id":       nodeID,
				"new_name": newName,
				"old_name": originalNode.Name,
			}
			return q.LogEvent(r.Context(), claims.UserID, "node_renamed", payload)
		})

		if txErr != nil {
			if errors.Is(txErr, database.ErrDuplicateNodeName) {
				http.Error(w, txErr.Error(), http.StatusConflict)
				return
			}
			if errors.Is(txErr, database.ErrNodeNotFound) {
				http.Error(w, "Node not found or you do not have permission to modify it", http.StatusNotFound)
				return
			}
			http.Error(w, "Failed to rename node", http.StatusInternalServerError)
			return
		}

		payload := map[string]interface{}{"id": nodeID, "new_name": newName, "old_name": originalNode.Name}
		eventMsg := map[string]interface{}{"event_type": "node_renamed", "payload": payload}
		eventBytes, _ := json.Marshal(eventMsg)
		s.wsHub.PublishEvent(claims.UserID, eventBytes)

		updated = true
	}

	if req.ParentID != nil {
		newParentID := *req.ParentID
		if len(newParentID) != 21 {
			http.Error(w, "Invalid ParentID format", http.StatusBadRequest)
			return
		}

		if originalNode.NodeType == "folder" {
			isCircular, err := s.store.IsDescendantOf(r.Context(), nodeID, newParentID)
			if err != nil {
				http.Error(w, "Failed to validate move operation", http.StatusInternalServerError)
				return
			}
			if isCircular {
				http.Error(w, "Cannot move a folder into itself or one of its subfolders", http.StatusBadRequest)
				return
			}
		}

		txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
			success, err := q.MoveNode(r.Context(), nodeID, claims.UserID, &newParentID)
			if err != nil {
				return err
			}
			if !success {
				return database.ErrNodeNotFound
			}

			payload := map[string]interface{}{
				"id":            nodeID,
				"new_parent_id": newParentID,
				"old_parent_id": originalNode.ParentID,
			}
			return q.LogEvent(r.Context(), claims.UserID, "node_moved", payload)
		})

		if txErr != nil {
			if errors.Is(txErr, database.ErrDuplicateNodeName) {
				http.Error(w, "A node with the same name already exists in the target folder", http.StatusConflict)
				return
			}
			if strings.Contains(txErr.Error(), "target folder does not exist") {
				http.Error(w, txErr.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, "Failed to move node", http.StatusInternalServerError)
			return
		}

		payload := map[string]interface{}{"id": nodeID, "new_parent_id": newParentID, "old_parent_id": originalNode.ParentID}
		eventMsg := map[string]interface{}{"event_type": "node_moved", "payload": payload}
		eventBytes, _ := json.Marshal(eventMsg)
		s.wsHub.PublishEvent(claims.UserID, eventBytes)

		updated = true
	}

	if !updated {
		http.Error(w, "No update operation specified (provide 'name' or 'parent_id')", http.StatusBadRequest)
		return
	}

	updatedNode, _ := s.store.GetNodeByID(r.Context(), nodeID, claims.UserID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(updatedNode)
}

// DownloadArchiveHandler obsługuje pobieranie wielu plików/folderów jako archiwum ZIP.
// @Summary      Download an archive
// @Description  Downloads multiple files and/or folders as a single ZIP archive.
// @Tags         nodes
// @Produce      application/zip
// @Security     BearerAuth
// @Param        ids    query     string  true  "Comma-separated list of Node IDs to include in the archive"
// @Success      200    {file}    binary  "The ZIP archive content"
// @Failure      400    {string}  string "Bad Request"
// @Failure      401    {string}  string "Unauthorized"
// @Failure      404    {string}  string "Not Found - one of the nodes does not exist"
// @Failure      500    {string}  string "Internal Server Error"
// @Router       /nodes/archive [get]
func (s *Server) DownloadArchiveHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	idsQuery := r.URL.Query().Get("ids")
	if idsQuery == "" {
		http.Error(w, "Node IDs are required", http.StatusBadRequest)
		return
	}
	nodeIDs := strings.Split(idsQuery, ",")

	nodesToPack := make(map[string]models.Node)
	nodePaths := make(map[string]string)

	var collectNodes func(nodeID, currentPath string) error
	collectNodes = func(nodeID, currentPath string) error {
		if _, exists := nodesToPack[nodeID]; exists {
			return nil
		}

		node, err := s.store.GetNodeByID(r.Context(), nodeID, claims.UserID)
		if err != nil {
			return fmt.Errorf("database error for node %s: %w", nodeID, err)
		}
		if node == nil {
			return fmt.Errorf("node with ID %s not found or you do not have permission to access it", nodeID)
		}

		fullPath := path.Join(currentPath, node.Name)
		nodesToPack[node.ID] = *node
		nodePaths[node.ID] = fullPath

		if node.NodeType == "folder" {
			children, err := s.store.GetNodesByParentID(r.Context(), claims.UserID, &node.ID)
			if err != nil {
				return fmt.Errorf("could not list children of folder %s: %w", node.Name, err)
			}
			for _, child := range children {
				if err := collectNodes(child.ID, fullPath); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, id := range nodeIDs {
		if err := collectNodes(id, ""); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="archive.zip"`)

	zipWriter := zip.NewWriter(w)
	defer zipWriter.Close()

	for id, node := range nodesToPack {
		fullPath := nodePaths[id]

		if node.NodeType == "folder" {
			zipWriter.Create(fullPath + "/")
		} else {
			fileWriter, err := zipWriter.Create(fullPath)
			if err != nil {
				log.Printf("ERROR creating entry in zip for %s: %v", node.Name, err)
				continue
			}
			fileStream, err := s.storage.Get(node.ID)
			if err != nil {
				log.Printf("ERROR getting file stream for %s: %v", node.Name, err)
				continue
			}
			io.Copy(fileWriter, fileStream)
			fileStream.Close()
		}
	}
}
