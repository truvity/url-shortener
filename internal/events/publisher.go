package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// The headers a published event carries beside its body.
const (
	HeaderDetailType = "X-Detail-Type"
	HeaderSource     = "X-Source"
)

// Publisher is what a component needs in order to emit an event: one method.
//
// The framework this example replaced offered four — publish one, publish
// many, shut down, health-check — and the service used one of them. A narrow
// interface is not tidiness: it is what lets a test substitute five lines
// instead of a mock, and what stops a caller reaching for a lifecycle method
// that belongs to whoever constructed the connection.
type Publisher interface {
	PublishEvents(ctx context.Context, events []Event) error
}

// Event is one message. Subject and Detail are the message; MsgID is what
// makes a retry idempotent on the server, and Source says who sent it.
type Event struct {
	Subject string `json:"subject"`
	// DetailType names the shape of Detail, so a consumer can tell one
	// kind of event from another without unmarshalling it twice.
	DetailType string `json:"detail_type,omitempty"`
	Detail     any    `json:"detail"`
	MsgID      string `json:"msg_id,omitempty"`
	Source     string `json:"source,omitempty"`
}

// JetStream publishes to a NATS JetStream stream.
type JetStream struct {
	js jetstream.JetStream
}

// NewJetStream returns a publisher over an established JetStream context.
// The connection belongs to whoever constructed it — this type does not close
// it, because a publisher that can close the connection is a publisher any
// caller can use to break every other publisher.
func NewJetStream(js jetstream.JetStream) *JetStream {
	return &JetStream{js: js}
}

// PublishEvents publishes each event and waits for the server to acknowledge
// it.
//
// Waiting is the point: an unacknowledged publish to a stream is a message
// that may or may not exist, and a click that may or may not have been
// counted is worse than one that was not.
//
// The BODY is the detail, and the type travels as a header. That split is not
// arbitrary — it is the fix for a bug the framework version had. There, the
// envelope's type field was dropped at publish time because only the detail
// was marshalled, so two kinds of event on one subject were indistinguishable
// to the consumer: it decoded a request event as a redirect event and counted
// a click for an empty URL. Keeping the body as the detail keeps every
// existing consumer working; putting the type in a header means it can no
// longer be silently lost.
func (p *JetStream) PublishEvents(ctx context.Context, events []Event) error {
	for _, e := range events {
		body, err := json.Marshal(e.Detail)
		if err != nil {
			return fmt.Errorf("marshal detail for %s: %w", e.Subject, err)
		}

		msg := &nats.Msg{Subject: e.Subject, Data: body, Header: nats.Header{}}
		if e.DetailType != "" {
			msg.Header.Set(HeaderDetailType, e.DetailType)
		}
		if e.Source != "" {
			msg.Header.Set(HeaderSource, e.Source)
		}

		// The trace context rides in the message, so a consumer's span
		// can name the request that caused it. Broker hops do not carry it
		// by themselves: without this the store shows a request that ends
		// at the publish and a consumer that began from nothing.
		//
		// Lower-case names, set on the map directly. NATS header names are
		// case-sensitive, and an HTTP-style carrier would canonicalise
		// `traceparent` to `Traceparent`, which a Kotlin or Python consumer
		// looking for the name the W3C specification gives would not find.
		carrier := propagation.MapCarrier{}
		otel.GetTextMapPropagator().Inject(ctx, carrier)
		for name, value := range carrier {
			msg.Header[name] = []string{value}
		}

		opts := []jetstream.PublishOpt{}
		if e.MsgID != "" {
			opts = append(opts, jetstream.WithMsgID(e.MsgID))
		}

		publishCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err = p.js.PublishMsg(publishCtx, msg, opts...)
		cancel()
		if err != nil {
			return fmt.Errorf("publish to %s: %w", e.Subject, err)
		}
	}
	return nil
}
