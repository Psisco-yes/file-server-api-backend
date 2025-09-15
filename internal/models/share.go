package models

import "time"

type Share struct {
	ID          int64     `json:"id"`
	NodeID      string    `json:"node_id"`
	SharerID    int64     `json:"sharer_id"`
	RecipientID int64     `json:"recipient_id"`
	Permissions string    `json:"permissions"`
	SharedAt    time.Time `json:"shared_at"`
}
