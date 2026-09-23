package models

import (
	"time"
)

type (
	// Stat represents click statistics for a URL
	Stat struct {
		// ID matches URL.ID (sha256(long_url)) for pairing
		ID string `gorm:"primaryKey;type:varchar(64);comment:sha256(long_url) - matches URL.ID"`

		// ClickCount is the total number of clicks (aggregate)
		ClickCount int64 `gorm:"not null;default:0;comment:Total click count"`

		// LastClickAt is the timestamp of the last click
		LastClickAt *time.Time `gorm:"comment:Last click timestamp"`
	}
)

// TableName overrides the table name.
// Schema-qualified, so the table lives in a schema this product owns
// rather than in whatever `public` happens to contain.
func (Stat) TableName() string {
	return "stats.stats" // PostgreSQL: {schema}.{table}
}
