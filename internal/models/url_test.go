package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestURL_TableName(t *testing.T) {
	url := URL{}

	// Test PostgreSQL mode (schema-qualified) - always used
	assert.Equal(t, "urls.urls", url.TableName(), "PostgreSQL should use schema-qualified name")
}

func TestURL_Structure(t *testing.T) {
	now := time.Now()
	expiresAt := now.Add(24 * time.Hour)

	url := URL{
		ID:        "abc123",
		URLKey:    "short123",
		LongURL:   "https://example.com/very/long/url",
		CreatedAt: now,
		UpdatedAt: &now,
		DeletedAt: nil,
		ExpiresAt: &expiresAt,
	}

	assert.Equal(t, "abc123", url.ID)
	assert.Equal(t, "short123", url.URLKey)
	assert.Equal(t, "https://example.com/very/long/url", url.LongURL)
	assert.Equal(t, now, url.CreatedAt)
	assert.NotNil(t, url.UpdatedAt)
	assert.Nil(t, url.DeletedAt)
	assert.NotNil(t, url.ExpiresAt)
}

func TestURL_SoftDelete(t *testing.T) {
	now := time.Now()
	url := URL{
		ID:        "abc123",
		URLKey:    "short123",
		LongURL:   "https://example.com",
		CreatedAt: now,
		DeletedAt: &now,
	}

	assert.NotNil(t, url.DeletedAt, "DeletedAt should be set for soft delete")
}
