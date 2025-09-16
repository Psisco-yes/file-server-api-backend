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

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jaevor/go-nanoid"
)

type CreateFolderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parent_id"`
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

	node, err := s.store.CreateNode(r.Context(), params)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			switch pgErr.Code {
			case "23503": // foreign_key_violation
				http.Error(w, "Parent folder does not exist", http.StatusBadRequest)
				return
			case "23505": // unique_violation
				http.Error(w, "A folder with the same name already exists in this location", http.StatusConflict)
				return
			}
		}

		log.Printf("ERROR: Failed to create folder in database: %v", err)
		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	err = s.store.LogEvent(r.Context(), claims.UserID, "node_created", node)
	if err != nil {
		log.Printf("WARN: Failed to log 'node_created' event for node %s: %v", node.ID, err)
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(node)
}

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

		node, err := s.store.CreateNode(r.Context(), params)
		if err != nil {
			log.Printf("ERROR creating db record for file %s: %v", handler.Filename, err)
			log.Printf("Attempting to clean up orphaned file on disk: %s", nodeID)
			if cleanupErr := s.storage.Delete(nodeID); cleanupErr != nil {
				log.Printf("CRITICAL: Failed to clean up orphaned file %s: %v", nodeID, cleanupErr)
			}
			continue
		}

		err = s.store.LogEvent(r.Context(), claims.UserID, "node_created", node)
		if err != nil {
			log.Printf("WARN: Failed to log 'node_created' event for node %s: %v", node.ID, err)
		}

		createdNodes = append(createdNodes, *node)
	}

	if len(createdNodes) == 0 {
		http.Error(w, "None of the files could be processed", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(createdNodes)
}

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

func (s *Server) DeleteNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

	if nodeID == "" {
		http.Error(w, "Node ID is required", http.StatusBadRequest)
		return
	}

	success, err := s.store.MoveNodeToTrash(r.Context(), nodeID, claims.UserID)
	if err != nil {
		http.Error(w, "Failed to delete node", http.StatusInternalServerError)
		return
	}

	if !success {
		http.Error(w, "Node not found or you do not have permission to delete it", http.StatusNotFound)
		return
	}

	payload := map[string]string{"id": nodeID}
	s.store.LogEvent(r.Context(), claims.UserID, "node_trashed", payload)

	w.WriteHeader(http.StatusNoContent)
}

type UpdateNodeRequest struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
}

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

		success, err := s.store.RenameNode(r.Context(), nodeID, claims.UserID, newName)
		if err != nil {
			if errors.Is(err, database.ErrDuplicateNodeName) {
				http.Error(w, err.Error(), http.StatusConflict)
				return
			}
			http.Error(w, "Failed to rename node", http.StatusInternalServerError)
			return
		}

		if !success {
			http.Error(w, "Node not found or you do not have permission to modify it", http.StatusNotFound)
			return
		}

		payload := map[string]interface{}{
			"id":       nodeID,
			"new_name": newName,
			"old_name": originalNode.Name,
		}
		s.store.LogEvent(r.Context(), claims.UserID, "node_renamed", payload)
		updated = true
	}

	if req.ParentID != nil {
		if len(*req.ParentID) != 21 {
			http.Error(w, "Invalid ParentID format", http.StatusBadRequest)
			return
		}

		success, err := s.store.MoveNode(r.Context(), nodeID, claims.UserID, req.ParentID)
		if err != nil {
			if errors.Is(err, database.ErrDuplicateNodeName) {
				http.Error(w, "A node with the same name already exists in the target folder", http.StatusConflict)
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if !success {
			http.Error(w, "Node not found or you do not have permission to modify it", http.StatusNotFound)
			return
		}

		payload := map[string]interface{}{
			"id":            nodeID,
			"new_parent_id": *req.ParentID,
			"old_parent_id": originalNode.ParentID,
		}
		s.store.LogEvent(r.Context(), claims.UserID, "node_moved", payload)
		updated = true
	}

	if !updated {
		http.Error(w, "No update operation specified (provide 'name' or 'parent_id')", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}

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
