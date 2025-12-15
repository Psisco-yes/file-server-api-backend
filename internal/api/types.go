package api

import (
	"encoding/json"
	"time"
)

type LoginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"password123"`
}

type TokenResponse struct {
	AccessToken  string `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	RefreshToken string `json:"refresh_token" example:"V1StGXR8_Z5jdHi6B-myT78q..."`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" example:"V1StGXR8_Z5jdHi6B-myT78q..."`
}

type CreateFolderRequest struct {
	Name     string  `json:"name" example:"Nowy Folder"`
	ParentID *string `json:"parent_id,omitempty" example:"_vx2a-43VqRT5wz_s9u4"`
}

type UpdateNodeRequest struct {
	Name     *string `json:"name,omitempty" example:"Nowa Nazwa Pliku"`
	ParentID *string `json:"parent_id,omitempty" example:"bNowyFolderRodzic123"`
}

type CopyNodeRequest struct {
	ParentID string  `json:"parent_id" example:"target_folder_id"`
	NewName  *string `json:"new_name,omitempty" example:"Kopia Raportu"`
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

type OutgoingShareResponse struct {
	ID                int64     `json:"id" example:"42"`
	NodeID            string    `json:"node_id" example:"_vx2a-43VqRT5wz_s9u4"`
	NodeName          string    `json:"node_name" example:"Wspólny Projekt"`
	NodeType          string    `json:"node_type" example:"folder"`
	RecipientUsername string    `json:"recipient_username" example:"user2"`
	Permissions       string    `json:"permissions" example:"write"`
	SharedAt          time.Time `json:"shared_at"`
}

type ShareResponse struct {
	ID          int64     `json:"id" example:"42"`
	NodeID      string    `json:"node_id" example:"_vx2a-43VqRT5wz_s9u4"`
	SharerID    int64     `json:"sharer_id" example:"1"`
	RecipientID int64     `json:"recipient_id" example:"2"`
	Permissions string    `json:"permissions" example:"read"`
	SharedAt    time.Time `json:"shared_at"`
}

type StorageUsageResponse struct {
	UsedBytes  int64 `json:"used_bytes"`
	QuotaBytes int64 `json:"quota_bytes"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" example:"password123"`
	NewPassword string `json:"new_password" example:"newStrongPassword456"`
}

type UpdateMeRequest struct {
	DisplayName *string `json:"display_name,omitempty" example:"Jan Kowalski"`
}

type EventResponse struct {
	ID        int64           `json:"id" example:"123"`
	EventType string          `json:"event_type" example:"node_created"`
	EventTime time.Time       `json:"event_time"`
	Payload   json.RawMessage `json:"payload" swaggertype:"object"`
}

type LatestEventResponse struct {
	LatestEventID int64 `json:"latest_event_id" example:"12345"`
}
