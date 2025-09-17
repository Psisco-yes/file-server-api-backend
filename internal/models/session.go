package models

import (
	"time"

	"github.com/google/uuid"
)

type Session struct {
	ID        uuid.UUID `json:"id" example:"a1b2c3d4-e5f6-7890-1234-567890abcdef"`
	UserAgent string    `json:"user_agent" example:"Mozilla/5.0 (Windows NT 10.0; Win64; x64) ..."`
	ClientIP  string    `json:"client_ip" example:"198.51.100.10"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}
