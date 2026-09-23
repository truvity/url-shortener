package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestStat_TableName(t *testing.T) {
	stat := Stat{}

	// Test PostgreSQL mode (schema-qualified) - always used
	assert.Equal(t, "stats.stats", stat.TableName(), "PostgreSQL should use schema-qualified name")
}

func TestStat_Structure(t *testing.T) {
	now := time.Now()

	stat := Stat{
		ID:          "abc123",
		ClickCount:  42,
		LastClickAt: &now,
	}

	assert.Equal(t, "abc123", stat.ID)
	assert.Equal(t, int64(42), stat.ClickCount)
	assert.NotNil(t, stat.LastClickAt)
}

func TestStat_DefaultClickCount(t *testing.T) {
	stat := Stat{
		ID: "abc123",
	}

	assert.Equal(t, int64(0), stat.ClickCount, "ClickCount should default to 0")
}
