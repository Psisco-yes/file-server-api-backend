package models

import "time"

type User struct {
	ID                int64     `json:"id" db:"id"`
	Username          string    `json:"username" db:"username"`
	PasswordHash      string    `json:"-" db:"password_hash"`
	DisplayName       *string   `json:"display_name,omitempty" db:"display_name"`
	CreatedAt         time.Time `json:"created_at" db:"created_at"`
	StorageQuotaBytes int64     `json:"storage_quota_bytes" db:"storage_quota_bytes"`
	StorageUsedBytes  int64     `json:"storage_used_bytes" db:"storage_used_bytes"`
}
