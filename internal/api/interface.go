package api

import (
	"context"
)

// ============================================================================
// API Interfaces - Match API Design Endpoints
// ============================================================================

type (
	// RedirectInterface defines the interface for redirecting short URLs.
	// Matches API endpoint: GET /urls/{url_key} (redirect)
	// Events are emitted automatically for statistics (URLRedirect and URLRequest events)
	RedirectInterface interface {
		// Redirect redirects to the long URL for the given short URL key.
		// Returns the long URL or an error if not found.
		// Automatically emits EventBridge events for statistics.
		Redirect(ctx context.Context, urlKey string) (string, error)
	}
)
