package events

import (
	"time"
)

// The events this product publishes.
//
// A subject and a detail type are part of the contract between two services,
// so they are named here once and referenced everywhere. A publisher that
// spells its own subject inline is a publisher a consumer stops matching
// after a typo nobody reviews.
const (
	// EventSourceRedirect is the event source for redirect events
	EventSourceRedirect = "url-shortener.redirect"
	// EventSourceWeb is the event source for web events
	EventSourceWeb = "url-shortener.web"

	// EventDetailTypeURLRedirect is the detail type for URL redirect events
	EventDetailTypeURLRedirect = "URLRedirect"
	// EventDetailTypeURLRequest is the detail type for URL request events
	EventDetailTypeURLRequest = "URLRequest"
)

type (
	// URLRedirectEventDetail represents the detail payload for a URLRedirect event
	URLRedirectEventDetail struct {
		URLKey    string              `json:"url_key"`
		LongURL   string              `json:"long_url"`
		Timestamp time.Time           `json:"timestamp"`
		UserUUID  string              `json:"user_uuid,omitempty"`
		Request   URLRedirectRequest  `json:"request"`
		Response  URLRedirectResponse `json:"response"`
	}

	// URLRedirectRequest contains request information for a redirect event
	URLRedirectRequest struct {
		ClientIP  string `json:"client_ip"`
		UserAgent string `json:"user_agent"`
		Referer   string `json:"referer,omitempty"`
		Country   string `json:"country,omitempty"`
		Region    string `json:"region,omitempty"`
		City      string `json:"city,omitempty"`
	}

	// URLRedirectResponse contains response information for a redirect event
	URLRedirectResponse struct {
		StatusCode int `json:"status_code"` // 302, 404, 410
		LatencyMS  int `json:"latency_ms"`
	}

	// URLRequestEventDetail represents the detail payload for a URLRequest event
	URLRequestEventDetail struct {
		RequestID string `json:"request_id"`
		// The operation's ID from the OpenAPI description, so that a
		// reader of the event and a reader of the API description are
		// looking at the same name.
		RequestType string             `json:"request_type"`
		URLKey      string             `json:"url_key,omitempty"`
		Timestamp   time.Time          `json:"timestamp"`
		UserUUID    string             `json:"user_uuid,omitempty"`
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

	// URLRequestMetadata says which build of which component emitted the
	// event. The version is the one the service reports on its version
	// endpoint, so an event can be traced back to an image.
	URLRequestMetadata struct {
		Component string `json:"component,omitempty"`
		Version   string `json:"version,omitempty"`
	}

	// URLRedirectEvent is what a consumer receives: the DETAIL of a
	// URLRedirect event, because the type and the source travel as message
	// headers rather than inside the body.
	URLRedirectEvent = URLRedirectEventDetail
)
