package api

// ============================================================================
// HTTP Request/Response Wrappers - Huma Operation Types
// ============================================================================
//
// These types wrap the business request/response types with HTTP-level
// metadata (headers, status codes, etc.) for Huma operations.
//
// Pattern:
//   - {Operation}Input  - Full HTTP request (headers + body + path params)
//   - {Operation}Output - Full HTTP response (status + headers + body)
//
// Benefits:
//   - Type-safe and reusable
//   - Self-documenting via struct tags
//   - Easy to test independently
//   - Clear separation of HTTP vs business concerns
//   - No anonymous structs in handlers
// ============================================================================

type (
	// RedirectURLInput is the full HTTP request for GET /urls/{url_key}
	RedirectURLInput struct {
		URLKey    string `path:"url_key" doc:"Short URL key" minLength:"8" maxLength:"8"`
		UserAgent string `header:"User-Agent" doc:"Client user agent"`
		Referer   string `header:"Referer" doc:"Referring page"`
		// XForwardedFor carries the original client IP when the request
		// arrives through a proxy — in-cluster every redirect passes the
		// Envoy gateway, so RemoteAddr alone is the gateway's address.
		XForwardedFor string `header:"X-Forwarded-For" doc:"Original client IP chain (proxied requests)"`
	}

	// RedirectURLOutput is the full HTTP response for GET /urls/{url_key}
	RedirectURLOutput struct {
		// HTTP 302 redirect
		Location string `header:"Location" doc:"Redirect target URL"`
	}
)
