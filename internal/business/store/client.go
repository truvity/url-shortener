package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/truvity/policy/examples/url-shortener/internal/models"
)

type (
	// Client provides database operations for URL storage using GORM
	// Works with PostgreSQL (Aurora) via IAM authentication
	Client struct {
		db     *gorm.DB
		logger *slog.Logger
	}

	// URLInfo represents URL information from the store
	// Note: Code is used internally for the url_key, URLKey is the API field name
	URLInfo struct {
		Code      string // Internal: url_key
		URLKey    string // API field: url_key
		LongURL   string
		CreatedAt time.Time
		UpdatedAt *time.Time
		DeletedAt *time.Time
		ExpiresAt *time.Time
	}
)

// NewClient creates a new GORM-based store client
// Note: Migrations are handled separately by the migration package.
// The tables it reads are created by the migration, which runs to completion
// before any service starts.
func NewClient(
	ctx context.Context,
	logger *slog.Logger,
	db *gorm.DB,
) (*Client, error) {
	logger.InfoContext(ctx, "creating store client")

	return &Client{
		db:     db,
		logger: logger,
	}, nil
}

// hashLongURL calculates SHA256 hash of the long URL for deterministic ID
func hashLongURL(longURL string) string {
	hash := sha256.Sum256([]byte(longURL))
	return hex.EncodeToString(hash[:])
}

// PutURL stores a URL entry (idempotent)
// Note: Stats are NOT created here - they are created lazily by IncrementClickCount (upsert)
// when the first redirect happens, so a reader of the counters needs no
// rights to create them.
func (c *Client) PutURL(ctx context.Context, urlKey, longURL string, expiresAt *time.Time) error {
	// Calculate deterministic hash for ID (idempotency key)
	id := hashLongURL(longURL)

	// Check if URL already exists (idempotent)
	var existing models.URL
	err := c.db.WithContext(ctx).Where("id = ?", id).First(&existing).Error
	if err == nil {
		// URL already exists (idempotent success)
		c.logger.InfoContext(ctx, "URL already exists (idempotent)",
			slog.String("url_key", urlKey),
			slog.String("long_url", longURL),
			slog.String("id", id))
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to check existing URL: %w", err)
	}

	// Create URL
	url := models.URL{
		ID:        id,
		URLKey:    urlKey,
		LongURL:   longURL,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
	}
	if err := c.db.WithContext(ctx).Create(&url).Error; err != nil {
		return fmt.Errorf("failed to create URL: %w", err)
	}

	c.logger.InfoContext(ctx, "URL stored successfully",
		slog.String("url_key", urlKey),
		slog.String("long_url", longURL),
		slog.String("id", id))

	return nil
}

// GetURL retrieves URL info by url_key
func (c *Client) GetURL(ctx context.Context, urlKey string) (*URLInfo, error) {
	var url models.URL
	err := c.db.WithContext(ctx).Where("url_key = ?", urlKey).First(&url).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get URL: %w", err)
	}

	return &URLInfo{
		Code:      url.URLKey,
		URLKey:    url.URLKey,
		LongURL:   url.LongURL,
		CreatedAt: url.CreatedAt,
		UpdatedAt: url.UpdatedAt,
		DeletedAt: url.DeletedAt,
		ExpiresAt: url.ExpiresAt,
	}, nil
}

// ListURLs returns a page of URLs, newest first, and the key to resume from.
//
// Paged by KEYSET rather than by offset. An offset re-reads and discards
// everything before it, so the last page of a large table is the most
// expensive one and the cost grows with the data — and rows inserted while
// somebody pages shift every later offset, which silently skips records.
// Ordering by a unique column and resuming after the last one seen has
// neither problem.
//
// `url_key` is the cursor because it is unique and the table is ordered by
// it. Created-at would read better and is not unique, so two rows sharing a
// timestamp would either repeat or vanish at a page boundary.
func (c *Client) ListURLs(ctx context.Context, limit int, after string, includeDeleted bool) ([]URLInfo, error) {
	query := c.db.WithContext(ctx).Model(&models.URL{}).Order("url_key asc").Limit(limit)
	if after != "" {
		query = query.Where("url_key > ?", after)
	}
	if !includeDeleted {
		query = query.Where("deleted_at IS NULL")
	}

	var urls []models.URL
	if err := query.Find(&urls).Error; err != nil {
		return nil, fmt.Errorf("failed to list URLs: %w", err)
	}

	infos := make([]URLInfo, 0, len(urls))
	for _, url := range urls {
		infos = append(infos, URLInfo{
			Code:      url.URLKey,
			URLKey:    url.URLKey,
			LongURL:   url.LongURL,
			CreatedAt: url.CreatedAt,
			UpdatedAt: url.UpdatedAt,
			DeletedAt: url.DeletedAt,
			ExpiresAt: url.ExpiresAt,
		})
	}

	return infos, nil
}

// GetURLByLongURL retrieves the entry for a long URL, or nil if there is none.
//
// The row's identity IS the hash of its long URL, so this is a primary-key
// lookup, not a scan -- and it is the question Create has to ask before it
// decides anything, because PutURL treats a URL that already exists as a
// success and stores nothing. A caller that skips this and reads back the
// key it just generated is reading a key that was never written.
//
// A retired entry is returned like any other: it still occupies the row, so
// the URL cannot be shortened again, and the caller has to be told that
// rather than shown a success.
func (c *Client) GetURLByLongURL(ctx context.Context, longURL string) (*URLInfo, error) {
	var url models.URL
	err := c.db.WithContext(ctx).Where("id = ?", hashLongURL(longURL)).First(&url).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get URL by long URL: %w", err)
	}

	return &URLInfo{
		Code:      url.URLKey,
		URLKey:    url.URLKey,
		LongURL:   url.LongURL,
		CreatedAt: url.CreatedAt,
		UpdatedAt: url.UpdatedAt,
		DeletedAt: url.DeletedAt,
		ExpiresAt: url.ExpiresAt,
	}, nil
}

// GetURLString retrieves long URL string by url_key (for redirect)
func (c *Client) GetURLString(ctx context.Context, urlKey string) (string, error) {
	info, err := c.GetURL(ctx, urlKey)
	if err != nil {
		return "", err
	}
	if info == nil {
		return "", nil
	}
	if info.DeletedAt != nil {
		return "", fmt.Errorf("URL was deleted: %s", urlKey)
	}
	return info.LongURL, nil
}

// FindURLByLongURL finds URL by long_url (for idempotency check)
func (c *Client) FindURLByLongURL(ctx context.Context, longURL string) (*URLInfo, error) {
	id := hashLongURL(longURL)

	var url models.URL
	err := c.db.WithContext(ctx).Where("id = ?", id).First(&url).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to find URL: %w", err)
	}

	return &URLInfo{
		Code:      url.URLKey,
		URLKey:    url.URLKey,
		LongURL:   url.LongURL,
		CreatedAt: url.CreatedAt,
		UpdatedAt: url.UpdatedAt,
		DeletedAt: url.DeletedAt,
		ExpiresAt: url.ExpiresAt,
	}, nil
}

// GetClickCount retrieves click count for a URL
func (c *Client) GetClickCount(ctx context.Context, urlKey string) (int64, error) {
	// Get URL to find ID
	urlInfo, err := c.GetURL(ctx, urlKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get URL for click count: %w", err)
	}
	if urlInfo == nil {
		return 0, nil
	}

	id := hashLongURL(urlInfo.LongURL)

	var stat models.Stat
	err = c.db.WithContext(ctx).Where("id = ?", id).First(&stat).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get click count: %w", err)
	}

	return stat.ClickCount, nil
}

// IncrementClickCount increments click count (atomic)
// Uses upsert to handle the case where Stat record doesn't exist yet
func (c *Client) IncrementClickCount(ctx context.Context, longURL string) error {
	id := hashLongURL(longURL)
	now := time.Now()

	// Get table name from model (schema-qualified for PostgreSQL)
	tableName := models.Stat{}.TableName()

	// Use upsert: INSERT ... ON CONFLICT DO UPDATE
	// This handles the case where Stat record doesn't exist yet
	query := fmt.Sprintf(`
		INSERT INTO %s (id, click_count, last_click_at)
		VALUES (?, 1, ?)
		ON CONFLICT (id) DO UPDATE SET
			click_count = %s.click_count + 1,
			last_click_at = EXCLUDED.last_click_at
	`, tableName, tableName)

	result := c.db.WithContext(ctx).Exec(query, id, now)

	if result.Error != nil {
		return fmt.Errorf("failed to increment click count: %w", result.Error)
	}

	c.logger.DebugContext(ctx, "incremented click count", slog.String("id", id))
	return nil
}

// DeleteURL soft deletes a URL
func (c *Client) DeleteURL(ctx context.Context, urlKey string) error {
	now := time.Now()
	result := c.db.WithContext(ctx).
		Model(&models.URL{}).
		Where("url_key = ? AND deleted_at IS NULL", urlKey).
		Update("deleted_at", now)

	if result.Error != nil {
		return fmt.Errorf("failed to delete URL: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("URL not found or already deleted: %s", urlKey)
	}

	c.logger.InfoContext(ctx, "deleted URL", slog.String("url_key", urlKey))
	return nil
}

// RestoreURL restores a deleted URL
func (c *Client) RestoreURL(ctx context.Context, urlKey string) error {
	result := c.db.WithContext(ctx).
		Model(&models.URL{}).
		Where("url_key = ? AND deleted_at IS NOT NULL", urlKey).
		Updates(map[string]interface{}{
			"deleted_at": nil,
			"updated_at": time.Now(),
		})

	if result.Error != nil {
		return fmt.Errorf("failed to restore URL: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("URL not found or not deleted: %s", urlKey)
	}

	c.logger.InfoContext(ctx, "restored URL", slog.String("url_key", urlKey))
	return nil
}

// UpdateURL updates URL metadata (expires_at only, not long_url)
func (c *Client) UpdateURL(ctx context.Context, urlKey string, longURL *string, expiresAt *time.Time) (*URLInfo, error) {
	// Reject long_url changes (would change ID hash)
	if longURL != nil {
		return nil, fmt.Errorf("changing long_url is not allowed (ID is hash-based). Create new URL instead")
	}

	now := time.Now()
	updates := map[string]interface{}{
		"updated_at": now,
	}

	if expiresAt != nil {
		updates["expires_at"] = expiresAt
	} else {
		updates["expires_at"] = nil
	}

	result := c.db.WithContext(ctx).
		Model(&models.URL{}).
		Where("url_key = ? AND deleted_at IS NULL", urlKey).
		Updates(updates)

	if result.Error != nil {
		return nil, fmt.Errorf("failed to update URL: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, fmt.Errorf("URL not found or already deleted: %s", urlKey)
	}

	// Fetch updated URL
	return c.GetURL(ctx, urlKey)
}
