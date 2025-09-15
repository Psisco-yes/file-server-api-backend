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
