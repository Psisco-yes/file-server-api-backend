package api

import (
	"encoding/json"
	"serwer-plikow/internal/models"
	"time"
)

type LoginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"password123"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VyX2lkIjoxLCJ1c2VybmFtZSI6ImFkbWluIiwiZXhwIjoxNzM0NTQ3MjAwfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"`
	RefreshToken string `json:"refresh_token" example:"V1StGXR8_Z5jdHi6B-myT78qBTw_In4s-Wp-s_3_F3g"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" example:"V1StGXR8_Z5jdHi6B-myT78qBTw_In4s-Wp-s_3_F3g"`
}

type CreateFolderRequest struct {
	Name     string  `json:"name" example:"Nowy Folder"`
	ParentID *string `json:"parent_id,omitempty" example:"_vx2a-43VqRT5wz_s9u4"`
}

type UpdateNodeRequest struct {
	Name     *string `json:"name,omitempty" example:"Zmieniona Nazwa Pliku"`
	ParentID *string `json:"parent_id,omitempty" example:"fLW5kAh2ia9vYmjMnU4nZ"`
}

type CopyNodeRequest struct {
	ParentID string  `json:"parent_id" example:"fLW5kAh2ia9vYmjMnU4nZ"`
	NewName  *string `json:"new_name,omitempty" example:"Kopia Raportu Q3"`
}

type ShareRequest struct {
	RecipientUsername string `json:"recipient_username" example:"user2"`
	Permissions       string `json:"permissions" example:"read" enums:"read,write"`
}

type SharingUserResponse struct {
	ID          int64  `json:"id" example:"2"`
	Username    string `json:"username" example:"user2"`
	DisplayName string `json:"display_name" example:"Jan Kowalski"`
}

type ShareResponse struct {
	ID          int64     `json:"id" example:"42"`
	NodeID      string    `json:"node_id" example:"_vx2a-43VqRT5wz_s9u4"`
	SharerID    int64     `json:"sharer_id" example:"1"`
	RecipientID int64     `json:"recipient_id" example:"2"`
	Permissions string    `json:"permissions" example:"read"`
	SharedAt    time.Time `json:"shared_at" example:"2025-12-01T15:04:05Z"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" example:"password123"`
	NewPassword string `json:"new_password" example:"newStrongPassword456"`
}

type UpdateMeRequest struct {
	DisplayName *string `json:"display_name,omitempty" example:"Adam Nowak"`
}

type EventResponse struct {
	ID        int64           `json:"id" example:"123"`
	EventType string          `json:"event_type" example:"node_created"`
	EventTime time.Time       `json:"event_time" example:"2025-12-01T16:30:00Z"`
	Payload   json.RawMessage `json:"payload" swaggertype:"primitive,string" example:"{\"id\":\"new_node_123\",\"name\":\"New Document.docx\",\"node_type\":\"file\"}"`
}

type LatestEventResponse struct {
	LatestEventID int64 `json:"latest_event_id" example:"12345"`
}

type ShareDetailResponse struct {
	ID                int64     `json:"id" example:"42"`
	RecipientUsername string    `json:"recipient_username" example:"user2"`
	Permissions       string    `json:"permissions" example:"write"`
	SharedAt          time.Time `json:"shared_at" example:"2025-12-01T15:04:05Z"`
}

type NodeDetailResponse struct {
	models.RichNode
	Path   []*models.RichNode    `json:"path"`
	Shares []ShareDetailResponse `json:"shares,omitempty"`
}

type InitiateUploadRequest struct {
	ParentID *string `json:"parent_id,omitempty" example:"_vx2a-43VqRT5wz_s9u4"`
	Name     string  `json:"name" example:"annual_report.zip"`
	MimeType string  `json:"mime_type" example:"application/zip"`
	Size     int64   `json:"size" example:"1073741824"`
}

type InitiateUploadResponse struct {
	UploadID string `json:"upload_id" example:"a1b2c3d4-e5f6-7890-1234-567890abcdef"`
}
