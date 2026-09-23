package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/truvity/policy/examples/url-shortener/internal/api"
	"github.com/truvity/policy/examples/url-shortener/internal/events"
)

type (
	// contextKey is a type for context keys to avoid collisions.
	contextKey string

	// RequestInfo contains request information extracted from Huma context.
	RequestInfo struct {
		Method    string
		Path      string
		ClientIP  string
		UserAgent string
		StartTime time.Time
	}
)

const (
	requestInfoKey contextKey = "request_info"
	startTimeKey   contextKey = "start_time"
)

// GetRequestInfo extracts RequestInfo from context.Context.
func GetRequestInfo(ctx context.Context) *RequestInfo {
	if info, ok := ctx.Value(requestInfoKey).(*RequestInfo); ok {
		return info
	}
	return nil
}

// GetStartTime extracts start time from context.Context.
func GetStartTime(ctx context.Context) time.Time {
	if startTime, ok := ctx.Value(startTimeKey).(time.Time); ok {
		return startTime
	}
	return time.Now()
}

// NewURLRequestMiddleware creates Huma middleware that emits URLRequest events for all operations.
//
// The middleware:
//  1. Extracts request info (method, path, client IP, user agent) from huma.Context
//  2. Stores it in context.Context using huma.WithValue() for business logic if needed
//  3. Calls the next handler
//  4. After handler completes, emits URLRequest event with OperationID from OpenAPI operation
//
// Parameters:
//   - logger: Logger for error logging and event emission logging
//   - publisher: NATS publisher (required, must not be nil)
func NewURLRequestMiddleware(
	logger *slog.Logger,
	publisher events.Publisher,
) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		startTime := time.Now()

		// Extract request information from huma.Context
		method := ctx.Method()
		path := ctx.URL().Path

		// Same resolution as the redirect route — one helper, so a request
		// cannot report two different client IPs across its two events.
		clientIP := api.ClientIP(ctx.Header("X-Forwarded-For"), ctx.RemoteAddr())

		userAgent := ctx.Header("User-Agent")

		reqInfo := &RequestInfo{
			Method:    method,
			Path:      path,
			ClientIP:  clientIP,
			UserAgent: userAgent,
			StartTime: startTime,
		}

		// Store request info and start time in context using huma.WithValue
		ctx = huma.WithValue(ctx, requestInfoKey, reqInfo)
		ctx = huma.WithValue(ctx, startTimeKey, startTime)

		// Call next handler
		next(ctx)

		// After handler completes, emit URLRequest event for all operations
		op := ctx.Operation()
		if op == nil {
			return
		}

		operationID := op.OperationID
		if operationID == "" {
			return
		}

		statusCode := ctx.Status()
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		latencyMS := int(time.Since(startTime).Milliseconds())

		// Extract URL key from path params if available
		urlKey := ctx.Param("url_key")

		// Emit URLRequest event
		now := time.Now()
		requestEvent := events.Event{
			Source:     events.EventSourceWeb,
			DetailType: events.EventDetailTypeURLRequest,
			Detail: events.URLRequestEventDetail{
				RequestID:   fmt.Sprintf("req-%s-%d", operationID, now.UnixNano()),
				RequestType: operationID,
				URLKey:      urlKey,
				Timestamp:   now,
				Request: events.URLRequestRequest{
					Method:    reqInfo.Method,
					Path:      reqInfo.Path,
					ClientIP:  reqInfo.ClientIP,
					UserAgent: reqInfo.UserAgent,
				},
				Response: events.URLRequestResponse{
					StatusCode: statusCode,
					LatencyMS:  latencyMS,
				},
			},
		}

		// Emit event asynchronously (non-blocking)
		eventCtx, eventCancel := context.WithTimeout(ctx.Context(), 500*time.Millisecond)
		defer eventCancel()

		logger.InfoContext(ctx.Context(), "sending URLRequest event to publisher",
			slog.String("operation_id", operationID),
			slog.String("url_key", urlKey),
			slog.String("method", method),
			slog.String("path", path),
			slog.Int("status_code", statusCode),
		)

		if err := publisher.PublishEvents(eventCtx, []events.Event{requestEvent}); err != nil {
			logger.ErrorContext(ctx.Context(), "failed to emit request event",
				slog.String("operation_id", operationID),
				slog.String("url_key", urlKey),
				slog.Any("error", err))
		} else {
			logger.DebugContext(ctx.Context(), "URLRequest event sent successfully",
				slog.String("operation_id", operationID),
				slog.String("url_key", urlKey))
		}
	}
}
