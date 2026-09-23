package events

import (
	"time"
)

// ============================================================================
// EventBridge Event Types
// ============================================================================

const (
	// EventSourceRedirect is the event source for redirect events
	EventSourceRedirect = "url-shortener.redirect"
	// EventSourceWeb is the event source for web events
	EventSourceWeb = "url-shortener.web"

	// EventDetailTypeURLRedirect is the detail type for URL redirect events
	EventDetailTypeURLRedirect = "URLRedirect"
	// EventDetailTypeURLRequest is the detail type for URL request events
	EventDetailTypeURLRequest = "URLRequest"

	// DetailTypeURLRedirect is an alias for EventDetailTypeURLRedirect (for SQS handlers)
	DetailTypeURLRedirect = EventDetailTypeURLRedirect
	// DetailTypeURLRequest is an alias for EventDetailTypeURLRequest (for SQS handlers)
	DetailTypeURLRequest = EventDetailTypeURLRequest
)

type (
	// URLRedirectEventDetail represents the detail payload for a URLRedirect event
	URLRedirectEventDetail struct {
		URLKey    string              `json:"url_key"`
		LongURL   string              `json:"long_url"`
		Timestamp time.Time           `json:"timestamp"`
		UserUUID  string              `json:"user_uuid,omitempty"` // Phase 3
		Request   URLRedirectRequest  `json:"request"`
		Response  URLRedirectResponse `json:"response"`
	}

	// URLRedirectRequest contains request information for a redirect event
	URLRedirectRequest struct {
		ClientIP  string `json:"client_ip"`
		UserAgent string `json:"user_agent"`
		Referer   string `json:"referer,omitempty"`
		Country   string `json:"country,omitempty"` // Phase 3
		Region    string `json:"region,omitempty"`  // Phase 3
		City      string `json:"city,omitempty"`    // Phase 3
	}

	// URLRedirectResponse contains response information for a redirect event
	URLRedirectResponse struct {
		StatusCode int `json:"status_code"` // 302, 404, 410
		LatencyMS  int `json:"latency_ms"`
	}

	// URLRequestEventDetail represents the detail payload for a URLRequest event
	URLRequestEventDetail struct {
		RequestID   string             `json:"request_id"`
		RequestType string             `json:"request_type"` // OpenAPI OperationID (e.g., "CreateURL", "ListURLs", "UpdateURL")
		URLKey      string             `json:"url_key,omitempty"`
		Timestamp   time.Time          `json:"timestamp"`
		UserUUID    string             `json:"user_uuid,omitempty"` // Phase 3
		Request     URLRequestRequest  `json:"request"`
		Response    URLRequestResponse `json:"response"`
		Metadata    URLRequestMetadata `json:"metadata,omitempty"`
	}

	// URLRequestRequest contains HTTP request information
	URLRequestRequest struct {
		Method      string            `json:"method"`
		Path        string            `json:"path"`
		QueryParams map[string]string `json:"query_params,omitempty"`
		Headers     map[string]string `json:"headers,omitempty"`
		ClientIP    string            `json:"client_ip"`
		UserAgent   string            `json:"user_agent"`
		BodySize    int               `json:"body_size,omitempty"`
	}

	// URLRequestResponse contains HTTP response information
	URLRequestResponse struct {
		StatusCode int `json:"status_code"`
		LatencyMS  int `json:"latency_ms"`
		BodySize   int `json:"body_size,omitempty"`
	}

	// URLRequestMetadata contains Lambda execution metadata
	URLRequestMetadata struct {
		LambdaName     string `json:"lambda_name,omitempty"`
		LambdaVersion  string `json:"lambda_version,omitempty"`
		LambdaMemory   int    `json:"lambda_memory,omitempty"`
		LambdaDuration int    `json:"lambda_duration_ms,omitempty"`
	}

	// Type aliases for SQS handler consumption (unwrapped event details)
	// These are the same as the EventDetail types but with cleaner names

	// URLRedirectEvent is an alias for URLRedirectEventDetail
	URLRedirectEvent = URLRedirectEventDetail

	// URLRequestEvent is an alias for URLRequestEventDetail
	URLRequestEvent = URLRequestEventDetail
)
