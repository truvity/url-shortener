package models

import (
	"time"
)

type (
	// URL represents a shortened URL entry
	URL struct {
		// ID is sha256(long_url) for idempotency (64-char hex string)
		ID string `gorm:"primaryKey;type:varchar(64);comment:sha256(long_url)"`

		// URLKey is the user-facing short code (8 alphanumeric chars)
		URLKey string `gorm:"uniqueIndex:idx_url_key;not null;type:varchar(8);comment:Short code for redirect"`

		// LongURL is the original URL to redirect to
		LongURL string `gorm:"not null;type:text;comment:Original URL"`

		// Timestamps
		CreatedAt time.Time  `gorm:"not null;index:idx_created_at;comment:Creation timestamp"`
		UpdatedAt *time.Time `gorm:"comment:Last update timestamp"`

		// Soft delete: a key is retired, never reused.
		DeletedAt *time.Time `gorm:"index:idx_deleted_at;comment:Soft delete timestamp"`

		// Optional expiry, after which the key answers 410 rather than 302.
		ExpiresAt *time.Time `gorm:"index:idx_expires_at;comment:Expiration timestamp"`
	}
)

// TableName overrides the table name.
// Uses schema-qualified name for PostgreSQL (Aurora).
func (URL) TableName() string {
	return "urls.urls" // PostgreSQL: {schema}.{table}
}
