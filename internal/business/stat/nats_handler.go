package stat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go/jetstream"

	"github.com/truvity/policy/examples/url-shortener/internal/events"
)

// Handler processes stat events from NATS JetStream.
type (
	Handler struct {
		manager *Manager
		logger  *slog.Logger
	}
)

// NewHandler creates a new stat event handler.
func NewHandler(ctx context.Context, logger *slog.Logger, manager *Manager) *Handler {
	return &Handler{
		manager: manager,
		logger:  logger.With(slog.String("component", "stat-handler")),
	}
}

// HandleNATSMessage processes a NATS JetStream message for stat processing.
// The message payload is the Detail field from the publisher (JSON-encoded event).
func (h *Handler) HandleNATSMessage(ctx context.Context, msg jetstream.Msg) error {
	h.logger.DebugContext(ctx, "handling NATS message",
		slog.String("subject", msg.Subject()))

	// The NATS payload is the Detail field — a JSON-encoded event
	// Try to parse as URLRedirectEvent directly
	var event events.URLRedirectEvent
	if err := json.Unmarshal(msg.Data(), &event); err != nil {
		h.logger.ErrorContext(ctx, "failed to parse redirect event from NATS",
			slog.Any("error", err))
		// Term — poison pill, don't retry
		if termErr := msg.Term(); termErr != nil {
			h.logger.WarnContext(ctx, "failed to term poison message", slog.Any("error", termErr))
		}
		return nil
	}

	if err := h.manager.ProcessURLRedirectEvent(ctx, event); err != nil {
		return fmt.Errorf("process redirect event: %w", err)
	}

	h.logger.DebugContext(ctx, "NATS message handled successfully",
		slog.String("subject", msg.Subject()))

	return nil
}
