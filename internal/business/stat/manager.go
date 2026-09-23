package stat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/truvity/policy/examples/url-shortener/internal/events"
)

type (
	// ClickCounter is the slice of the store this manager uses. Declared at
	// the consumer so the empty-long_url guard below can be exercised without
	// a database; *store.Client satisfies it, so call sites pass it unchanged.
	ClickCounter interface {
		IncrementClickCount(ctx context.Context, longURL string) error
	}

	// Manager handles stat processing business logic
	Manager struct {
		store  ClickCounter
		logger *slog.Logger
	}
)

// NewManager creates a new stat manager
func NewManager(ctx context.Context, logger *slog.Logger, storeClient ClickCounter) *Manager {
	return &Manager{
		store:  storeClient,
		logger: logger.With(slog.String("component", "stat-manager")),
	}
}

// ProcessURLRedirectEvent processes a URL redirect event by incrementing click count
func (m *Manager) ProcessURLRedirectEvent(ctx context.Context, event events.URLRedirectEvent) error {
	m.logger.InfoContext(ctx, "processing URL redirect event",
		slog.String("url_key", event.URLKey),
		slog.String("long_url", event.LongURL),
		slog.Time("timestamp", event.Timestamp))

	// Guard: clicks are counted per long_url, so an empty one is never a real
	// redirect. nats-utils drops Event.DetailType on publish, so any foreign
	// payload on this stream decodes into a zero-valued URLRedirectEvent —
	// without this guard that silently increments a click for "".
	if event.LongURL == "" {
		m.logger.WarnContext(ctx, "skipping redirect event with empty long_url",
			slog.String("url_key", event.URLKey))
		return nil
	}

	if err := m.store.IncrementClickCount(ctx, event.LongURL); err != nil {
		m.logger.ErrorContext(ctx, "failed to increment click count",
			slog.String("url_key", event.URLKey),
			slog.String("long_url", event.LongURL),
			slog.Any("error", err))
		return fmt.Errorf("increment click count: %w", err)
	}

	m.logger.InfoContext(ctx, "URL redirect event processed successfully",
		slog.String("url_key", event.URLKey))

	return nil
}

// ProcessEvent handles one message off the stream.
// Determines event type and routes to appropriate handler
func (m *Manager) ProcessEvent(ctx context.Context, messageBody string) error {
	// Parse event
	var envelope struct {
		DetailType string          `json:"detail-type"`
		Detail     json.RawMessage `json:"detail"`
	}
	if err := json.Unmarshal([]byte(messageBody), &envelope); err != nil {
		m.logger.ErrorContext(ctx, "failed to parse event",
			slog.Any("error", err))
		return nil // Return nil to delete malformed message
	}

	m.logger.DebugContext(ctx, "received a message",
		slog.String("detail_type", envelope.DetailType))

	// Route based on detail-type
	switch envelope.DetailType {
	case events.EventDetailTypeURLRedirect:
		var event events.URLRedirectEvent
		if err := json.Unmarshal(envelope.Detail, &event); err != nil {
			m.logger.ErrorContext(ctx, "failed to parse URL redirect event",
				slog.Any("error", err))
			return nil // Return nil to delete malformed message
		}
		return m.ProcessURLRedirectEvent(ctx, event)

	default:
		m.logger.WarnContext(ctx, "unknown event type, skipping",
			slog.String("detail_type", envelope.DetailType))
		return nil // Return nil to delete unknown message
	}
}
