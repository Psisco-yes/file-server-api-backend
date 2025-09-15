package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"serwer-plikow/internal/database"
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
			if pgErr.Code == "23503" {
				http.Error(w, "Parent folder does not exist", http.StatusBadRequest)
				return
			}
			// TODO: Dodać obsługę błędu '23505' (unique_violation)
		}

		http.Error(w, "Failed to create folder", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(node)
}

func (s *Server) ListNodesHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	parentIDStr := r.URL.Query().Get("parent_id")

	var parentID *string
	if parentIDStr != "" {
		parentID = &parentIDStr
	}

	nodes, err := s.store.GetNodesByParentID(r.Context(), claims.UserID, parentID)
	if err != nil {
		http.Error(w, "Failed to list nodes", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}

func (s *Server) UploadFileHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, 1<<30)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, "Error parsing multipart form", http.StatusBadRequest)
		return
	}

	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Error retrieving the file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	parentIDStr := r.FormValue("parent_id")
	var parentID *string
	if parentIDStr != "" {
		if len(parentIDStr) != 21 {
			http.Error(w, "Invalid ParentID format", http.StatusBadRequest)
			return
		}
		parentID = &parentIDStr
	}

	nodeID, err := s.generateUniqueID(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := s.storage.Save(nodeID, file); err != nil {
		http.Error(w, "Failed to save file", http.StatusInternalServerError)
		return
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
		// TODO: Dodać logikę usuwania pliku z dysku, jeśli zapis do bazy się nie powiódł
		http.Error(w, "Failed to create file record", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(node)
}

func (s *Server) DownloadFileHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	nodeID := chi.URLParam(r, "nodeId")
	if nodeID == "" {
		http.Error(w, "Node ID is required", http.StatusBadRequest)
		return
	}

	node, err := s.store.GetNodeByID(r.Context(), nodeID, claims.UserID)
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

	w.WriteHeader(http.StatusNoContent)
}

type UpdateNodeRequest struct {
	Name     *string `json:"name"`
	ParentID *string `json:"parent_id"`
}

func (s *Server) UpdateNodeHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	nodeID := chi.URLParam(r, "nodeId")

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
		updated = true
	}

	if !updated {
		http.Error(w, "No update operation specified (provide 'name' or 'parent_id')", http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
}
