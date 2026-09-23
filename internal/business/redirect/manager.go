package redirect

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/truvity/policy/examples/url-shortener/internal/events"
)

type (
	// URLResolver is the slice of the store this manager uses. Declared at the
	// consumer so the event emission below can be exercised without a
	// database; *store.Client satisfies it, so call sites pass it unchanged.
	URLResolver interface {
		GetURLString(ctx context.Context, urlKey string) (string, error)
	}

	// Manager implements RedirectInterface
	Manager struct {
		store     URLResolver
		publisher events.Publisher
		logger    *slog.Logger
	}

	// RequestInfo is what the caller knows about the request that caused a
	// redirect, and what the emitted event carries.
	RequestInfo struct {
		ClientIP  string
		UserAgent string
		Referer   string
		StartTime time.Time
	}
)

// NewManager creates a new redirect manager
func NewManager(logger *slog.Logger, storeClient URLResolver, publisher events.Publisher) *Manager {
	return &Manager{
		store:     storeClient,
		publisher: publisher,
		logger:    logger,
	}
}

// RedirectWithInfo redirects with request information for event emission
func (m *Manager) RedirectWithInfo(ctx context.Context, urlKey string, reqInfo RequestInfo) (string, error) {
	startTime := reqInfo.StartTime
	if startTime.IsZero() {
		startTime = time.Now()
	}

	// Get URL from DynamoDB
	longURL, err := m.store.GetURLString(ctx, urlKey)
	if err != nil {
		return "", fmt.Errorf("failed to get URL: %w", err)
	}

	// Check if URL exists (empty string means not found)
	if longURL == "" {
		return "", fmt.Errorf("URL not found for url_key: %s", urlKey)
	}

	// Calculate latency
	latencyMS := int(time.Since(startTime).Milliseconds())

	// Emit events synchronously with timeout (must complete before Lambda handler returns)
	// Use a timeout to prevent publish delays from blocking redirect too long
	eventCtx, eventCancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer eventCancel()
	m.emitRedirectEvent(eventCtx, urlKey, longURL, reqInfo, latencyMS, http.StatusFound)

	m.logger.InfoContext(ctx, "redirecting URL",
		slog.String("url_key", urlKey),
		slog.String("long_url", longURL))

	return longURL, nil
}

// emitRedirectEvent emits the URLRedirect event.
//
// It publishes URLRedirect ONLY. It used to also put a URLRequest event on the
// same (redirect) publisher, which was both redundant and harmful:
// NewURLRequestMiddleware already emits URLRequest to the LOG publisher, and
// nats-utils drops Event.DetailType when publishing (only Detail is marshaled,
// to one subject), so the stat consumer cannot tell the two apart — it decoded
// the URLRequest payload as a URLRedirectEvent and incremented a click for an
// empty long_url. One event kind per stream keeps the consumer unambiguous.
func (m *Manager) emitRedirectEvent(
	ctx context.Context,
	urlKey string,
	longURL string,
	reqInfo RequestInfo,
	latencyMS int,
	statusCode int,
) {
	now := time.Now()

	// Emit URLRedirect event
	redirectEvent := events.Event{
		Source:     events.EventSourceRedirect,
		DetailType: events.EventDetailTypeURLRedirect,
		Detail: events.URLRedirectEventDetail{
			URLKey:    urlKey,
			LongURL:   longURL,
			Timestamp: now,
			Request: events.URLRedirectRequest{
				ClientIP:  reqInfo.ClientIP,
				UserAgent: reqInfo.UserAgent,
				Referer:   reqInfo.Referer,
			},
			Response: events.URLRedirectResponse{
				StatusCode: statusCode,
				LatencyMS:  latencyMS,
			},
		},
	}

	if err := m.publisher.PublishEvents(ctx, []events.Event{redirectEvent}); err != nil {
		// Log error but don't fail the redirect (events are non-critical)
		m.logger.ErrorContext(ctx, "failed to emit redirect event",
			slog.String("url_key", urlKey),
			slog.Any("error", err))
	} else {
		m.logger.DebugContext(ctx, "emitted redirect event",
			slog.String("url_key", urlKey))
	}
}

// Redirect implements RedirectInterface.
// This is the simple version that works without request information.
// For event emission with full request details, use RedirectWithInfo instead.
// Events are still emitted, but with minimal request information.
func (m *Manager) Redirect(ctx context.Context, urlKey string) (string, error) {
	return m.RedirectWithInfo(ctx, urlKey, RequestInfo{
		StartTime: time.Now(),
	})
}
