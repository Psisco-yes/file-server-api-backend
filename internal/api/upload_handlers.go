package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"serwer-plikow/internal/database"
	_ "serwer-plikow/internal/models"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// @Summary      Initiate a chunked file upload
// @Description  Starts a new upload session for a large file. Returns an upload_id to be used for subsequent chunk uploads.
// @Tags         uploads
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        uploadRequest  body      InitiateUploadRequest  true  "File metadata"
// @Success      201            {object}  InitiateUploadResponse
// @Failure      400            {string}  string "Bad Request - Invalid name or size"
// @Failure      403            {string}  string "Forbidden - Write permission denied"
// @Failure      404            {string}  string "Not Found - Parent folder not found"
// @Failure      409            {string}  string "Conflict - a file with the same name already exists"
// @Failure      413            {string}  string "Payload Too Large - Storage quota exceeded"
// @Failure      429            {string}  string "Too Many Requests"
// @Failure      500            {string}  string "Internal Server Error"
// @Router       /nodes/upload/initiate [post]
func (s *Server) InitiateUploadHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())

	var req InitiateUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if strings.TrimSpace(req.Name) == "" || req.Size <= 0 {
		http.Error(w, "Invalid file name or size", http.StatusBadRequest)
		return
	}

	var ownerID int64 = claims.UserID
	if req.ParentID != nil {
		parentFolder, err := s.store.GetNodeIfAccessible(r.Context(), *req.ParentID, claims.UserID)
		if err != nil || parentFolder == nil {
			http.Error(w, "Parent folder not found or access denied", http.StatusNotFound)
			return
		}
		ownerID = parentFolder.OwnerID
	}

	hasWritePermission, err := s.store.CheckWritePermission(r.Context(), claims.UserID, req.ParentID)
	if err != nil || !hasWritePermission {
		http.Error(w, "You do not have permission to create items in this folder", http.StatusForbidden)
		return
	}

	ownerUser, err := s.store.GetUserByID(r.Context(), ownerID)
	if err != nil || ownerUser == nil {
		http.Error(w, "Could not verify owner for quota check", http.StatusInternalServerError)
		return
	}
	if ownerUser.StorageUsedBytes+req.Size > ownerUser.StorageQuotaBytes {
		http.Error(w, "Storage quota for the owner of this folder would be exceeded", http.StatusRequestEntityTooLarge)
		return
	}

	uploadID := uuid.New()
	nodeID, err := s.generateUniqueID(r.Context())
	if err != nil {
		http.Error(w, "Failed to generate unique ID for node", http.StatusInternalServerError)
		return
	}

	params := database.CreateUploadParams{
		ID:             uploadID,
		NodeID:         nodeID,
		OwnerID:        ownerID,
		ParentID:       req.ParentID,
		Name:           req.Name,
		MimeType:       req.MimeType,
		TotalSizeBytes: req.Size,
	}

	if err := s.store.CreateUpload(r.Context(), params); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			http.Error(w, "A file with the same name already exists in this location", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to initiate upload session in database", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(InitiateUploadResponse{UploadID: uploadID.String()})
}

// @Summary      Upload a file chunk
// @Description  Uploads a single chunk of a file for a given upload_id. The 'Content-Range' header is required.
// @Tags         uploads
// @Accept       application/octet-stream
// @Security     BearerAuth
// @Param        uploadId   path      string  true  "The ID of the upload session"
// @Param        Content-Range header string true "Indicates the byte range of the chunk (e.g., 'bytes 0-1048575/4194304')"
// @Success      204      {null}    nil     "No Content"
// @Failure      400      {string}  string "Bad Request - Invalid Content-Range header"
// @Failure      401      {string}  string "Unauthorized"
// @Failure      403      {string}  string "Forbidden"
// @Failure      404      {string}  string "Not Found - Upload session not found"
// @Failure      416      {string}  string "Range Not Satisfiable - The chunk's start byte does not match the expected offset"
// @Failure      429      {string}  string "Too Many Requests"
// @Failure      500      {string}  string "Internal Server Error"
// @Router       /nodes/upload/{uploadId} [patch]
func (s *Server) UploadChunkHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	uploadID, err := uuid.Parse(chi.URLParam(r, "uploadId"))
	if err != nil {
		http.Error(w, "Invalid upload ID format", http.StatusBadRequest)
		return
	}

	upload, err := s.store.GetUploadByID(r.Context(), uploadID)
	if err != nil {
		http.Error(w, "Failed to retrieve upload session", http.StatusInternalServerError)
		return
	}
	if upload == nil {
		http.Error(w, "Upload session not found", http.StatusNotFound)
		return
	}

	if upload.OwnerID != claims.UserID {
		canWrite, _ := s.store.CheckWritePermission(r.Context(), claims.UserID, upload.ParentID)
		if !canWrite {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
	}

	contentRange := r.Header.Get("Content-Range")
	var start, end, total int64
	if contentRange != "" {
		_, err := fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total)
		if err != nil || total != upload.TotalSizeBytes {
			http.Error(w, "Invalid 'Content-Range' header", http.StatusBadRequest)
			return
		}
	} else {
		start = upload.UploadedBytes
	}

	if start != upload.UploadedBytes {
		http.Error(w, fmt.Sprintf("Range Not Satisfiable: expected next chunk to start at byte %d", upload.UploadedBytes), http.StatusRequestedRangeNotSatisfiable)
		return
	}

	tempFilePath := filepath.Join(s.storage.GetBasePath(), "tmp", upload.NodeID)

	if _, err := os.Stat(filepath.Dir(tempFilePath)); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(tempFilePath), 0755); err != nil {
			http.Error(w, "Failed to create temporary storage directory", http.StatusInternalServerError)
			return
		}
	}

	fileFlags := os.O_WRONLY
	if start == 0 {
		fileFlags |= os.O_CREATE | os.O_TRUNC
	}

	file, err := os.OpenFile(tempFilePath, fileFlags, 0644)
	if err != nil {
		http.Error(w, "Failed to open temporary file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	if _, err := file.Seek(start, io.SeekStart); err != nil {
		http.Error(w, "Failed to seek in temporary file", http.StatusInternalServerError)
		return
	}

	written, err := io.Copy(file, r.Body)
	if err != nil {
		http.Error(w, "Failed to write chunk to temporary file", http.StatusInternalServerError)
		return
	}

	newUploadedBytes := upload.UploadedBytes + written
	if err := s.store.UpdateUploadProgress(r.Context(), uploadID, newUploadedBytes); err != nil {
		http.Error(w, "Failed to update upload progress", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// @Summary      Complete a chunked upload
// @Description  Finalizes a chunked upload after all chunks have been sent. The server verifies the file and moves it to permanent storage.
// @Tags         uploads
// @Produce      json
// @Security     BearerAuth
// @Param        uploadId   path      string  true  "The ID of the upload session"
// @Success      201        {object}  models.RichNode
// @Failure      400        {string}  string "Bad Request - Upload incomplete or file mismatch"
// @Failure      401        {string}  string "Unauthorized"
// @Failure      403        {string}  string "Forbidden"
// @Failure      404        {string}  string "Not Found - Upload session not found"
// @Failure      429        {string}  string "Too Many Requests"
// @Failure      500        {string}  string "Internal Server Error"
// @Router       /nodes/upload/{uploadId}/complete [post]
func (s *Server) CompleteUploadHandler(w http.ResponseWriter, r *http.Request) {
	claims := GetUserFromContext(r.Context())
	uploadID, err := uuid.Parse(chi.URLParam(r, "uploadId"))
	if err != nil {
		http.Error(w, "Invalid upload ID format", http.StatusBadRequest)
		return
	}

	var finalNodeID string
	var ownerNotifiedID *int64

	txErr := s.store.ExecTx(r.Context(), func(q *database.Queries) error {
		upload, err := q.GetUploadByID(r.Context(), uploadID)
		if err != nil {
			return fmt.Errorf("database error: %w", err)
		}
		if upload == nil {
			return errors.New("upload session not found")
		}

		finalNodeID = upload.NodeID

		if upload.OwnerID != claims.UserID {
			canWrite, _ := q.CheckWritePermission(r.Context(), claims.UserID, upload.ParentID)
			if !canWrite {
				return errors.New("forbidden")
			}
		}

		if upload.UploadedBytes != upload.TotalSizeBytes {
			return fmt.Errorf("upload is incomplete: %d/%d bytes have been uploaded", upload.UploadedBytes, upload.TotalSizeBytes)
		}

		tempFilePath := filepath.Join(s.storage.GetBasePath(), "tmp", upload.NodeID)
		if err := s.storage.Move(tempFilePath, upload.NodeID); err != nil {
			return fmt.Errorf("failed to move file to permanent storage: %w", err)
		}

		sizeBytes := upload.TotalSizeBytes
		mimeType := upload.MimeType
		nodeParams := database.CreateNodeParams{
			ID:        upload.NodeID,
			OwnerID:   upload.OwnerID,
			ParentID:  upload.ParentID,
			Name:      upload.Name,
			NodeType:  "file",
			SizeBytes: &sizeBytes,
			MimeType:  &mimeType,
		}
		createdNode, err := q.CreateNode(r.Context(), nodeParams)
		if err != nil {
			return fmt.Errorf("failed to create node record: %w", err)
		}

		if err := q.UpdateUserStorage(r.Context(), upload.OwnerID, upload.TotalSizeBytes); err != nil {
			return fmt.Errorf("failed to update user storage: %w", err)
		}

		if err := q.DeleteUpload(r.Context(), uploadID); err != nil {
			return fmt.Errorf("failed to clean up upload session: %w", err)
		}

		if err := q.LogEvent(r.Context(), claims.UserID, "node_created", createdNode); err != nil {
			return err
		}
		if upload.OwnerID != claims.UserID {
			ownerNotifiedID = &upload.OwnerID
			return q.LogEvent(r.Context(), *ownerNotifiedID, "node_created", createdNode)
		}
		return nil
	})

	if txErr != nil {
		errMsg := txErr.Error()
		switch errMsg {
		case "upload session not found":
			http.Error(w, errMsg, http.StatusNotFound)
		case "forbidden":
			http.Error(w, errMsg, http.StatusForbidden)
		default:
			http.Error(w, errMsg, http.StatusBadRequest)
		}
		return
	}

	finalRichNode, err := s.store.GetRichNodeIfAccessible(r.Context(), finalNodeID, claims.UserID)
	if err != nil || finalRichNode == nil {
		log.Printf("CRITICAL: Could not retrieve rich node for completed upload %s", finalNodeID)
		http.Error(w, "File was uploaded but its details could not be retrieved", http.StatusInternalServerError)
		return
	}

	eventMsg := map[string]interface{}{"event_type": "node_created", "payload": finalRichNode}
	eventBytes, _ := json.Marshal(eventMsg)
	s.wsHub.PublishEvent(claims.UserID, eventBytes)
	if ownerNotifiedID != nil {
		s.wsHub.PublishEvent(*ownerNotifiedID, eventBytes)
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(finalRichNode)
}
