package models

import "time"

type Node struct {
	ID               string     `json:"id"`
	OwnerID          int64      `json:"owner_id"`
	ParentID         *string    `json:"parent_id"`
	Name             string     `json:"name"`
	NodeType         string     `json:"node_type"`
	SizeBytes        *int64     `json:"size_bytes"`
	MimeType         *string    `json:"mime_type"`
	CreatedAt        time.Time  `json:"created_at"`
	ModifiedAt       time.Time  `json:"modified_at"`
	DeletedAt        *time.Time `json:"deleted_at,omitempty"`
	OriginalParentID *string    `json:"-"`
}

type RichNodeOwner struct {
	ID          int64   `json:"id" example:"1"`
	Username    string  `json:"username" example:"admin"`
	DisplayName *string `json:"display_name,omitempty" example:"Administrator"`
}

type RichNode struct {
	ID          string        `json:"id" example:"_vx2a-43VqRT5wz_s9u4"`
	ParentID    *string       `json:"parent_id,omitempty" example:"fLW5kAh2ia9vYmjMnU4nZ"`
	Name        string        `json:"name" example:"Raport_Q3.docx"`
	NodeType    string        `json:"node_type" example:"file"`
	SizeBytes   *int64        `json:"size_bytes,omitempty" example:"123456"`
	MimeType    *string       `json:"mime_type,omitempty" example:"application/pdf"`
	CreatedAt   time.Time     `json:"created_at"`
	ModifiedAt  time.Time     `json:"modified_at"`
	Owner       RichNodeOwner `json:"owner"`
	IsFavorited bool          `json:"is_favorited" example:"true"`
	IsShared    bool          `json:"is_shared" example:"false"`
}
