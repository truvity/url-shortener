package runtime

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

// TraceDatabase gives every query a span, as a child of the request that
// asked.
//
// Without it the trace ends at the service boundary: a slow request shows one
// span with nothing beneath it, and the question "was it the database?" has
// no answer in the store that exists to answer it.
//
// Written here rather than taken from a library. The published plugin
// imports every database driver its author supports, so a service that
// speaks to one database ships the client for four -- and one of them
// carried a known vulnerability the service could reach through nothing
// but the import.
//
// The statement is recorded as its TEXT, with the placeholders in it, and
// never with the arguments: those are user data -- here, the long URLs
// people shorten -- and a trace store is not where they belong.
func TraceDatabase(db *gorm.DB) error {
	if err := db.Use(tracing{tracer: otel.GetTracerProvider().Tracer("gorm")}); err != nil {
		return fmt.Errorf("trace the database: %w", err)
	}
	return nil
}

type tracing struct{ tracer trace.Tracer }

func (tracing) Name() string { return "url-shortener:tracing" }

// running is what the before callback leaves for the after one: the span,
// and the context to put back so the statement is left as it was found.
type running struct {
	span   trace.Span
	parent context.Context
}

const runningKey = "url-shortener:span"

func (t tracing) Initialize(db *gorm.DB) error {
	cb := db.Callback()
	steps := []struct {
		name   string
		before func(string, func(*gorm.DB)) error
		after  func(string, func(*gorm.DB)) error
	}{
		{"create", cb.Create().Before("gorm:create").Register, cb.Create().After("gorm:create").Register},
		{"query", cb.Query().Before("gorm:query").Register, cb.Query().After("gorm:query").Register},
		{"update", cb.Update().Before("gorm:update").Register, cb.Update().After("gorm:update").Register},
		{"delete", cb.Delete().Before("gorm:delete").Register, cb.Delete().After("gorm:delete").Register},
		{"row", cb.Row().Before("gorm:row").Register, cb.Row().After("gorm:row").Register},
		{"raw", cb.Raw().Before("gorm:raw").Register, cb.Raw().After("gorm:raw").Register},
	}
	for _, s := range steps {
		if err := s.before("url-shortener:before_"+s.name, t.start(s.name)); err != nil {
			return fmt.Errorf("register %s: %w", s.name, err)
		}
		if err := s.after("url-shortener:after_"+s.name, t.finish); err != nil {
			return fmt.Errorf("register %s: %w", s.name, err)
		}
	}
	return nil
}

func (t tracing) start(operation string) func(*gorm.DB) {
	return func(tx *gorm.DB) {
		parent := tx.Statement.Context
		if parent == nil {
			parent = context.Background()
		}
		// Named for the operation and the table, both bounded sets; the
		// statement text goes in an attribute because it is not.
		name := "gorm." + operation
		if table := tx.Statement.Table; table != "" {
			name += " " + table
		}
		ctx, span := t.tracer.Start(parent, name, trace.WithSpanKind(trace.SpanKindClient),
			trace.WithAttributes(
				attribute.String("db.system.name", "postgresql"),
				attribute.String("db.operation.name", operation),
			))
		tx.Statement.Context = ctx
		tx.InstanceSet(runningKey, running{span: span, parent: parent})
	}
}

func (tracing) finish(tx *gorm.DB) {
	value, ok := tx.InstanceGet(runningKey)
	if !ok {
		return
	}
	r, ok := value.(running)
	if !ok {
		return
	}
	tx.Statement.Context = r.parent
	defer r.span.End()

	r.span.SetAttributes(
		attribute.String("db.query.text", tx.Statement.SQL.String()),
		attribute.Int64("db.response.returned_rows", tx.RowsAffected),
	)
	// A missing row is an answer, not a failure: marking it an error would
	// turn every lookup for an unknown key into a red span.
	if err := tx.Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		r.span.RecordError(err)
		r.span.SetStatus(codes.Error, "query failed")
	}
}
