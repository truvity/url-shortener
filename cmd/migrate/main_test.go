package main

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// recorder installs an in-memory provider and returns what it collects.
func recorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	rec := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { otel.SetTracerProvider(previous) })

	return rec
}

// The run is a span, and it is the SAME context the step sees.
//
// A Job that starts telemetry and creates no span exports nothing at all:
// the provider is where spans go, not what makes them. That was this Job
// for as long as it existed, and it looked exactly like a Job that ran and
// was never observed.
func TestTheRunIsASpan(t *testing.T) {
	rec := recorder(t)

	var inner bool
	if err := withSpan(context.Background(), "migrate.run", func(ctx context.Context) error {
		inner = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	spans := rec.Ended()
	if len(spans) != 1 || spans[0].Name() != "migrate.run" {
		t.Fatalf("expected one span named migrate.run, got %d: %v", len(spans), spans)
	}
	if !inner {
		t.Error("the step did not run")
	}
	if spans[0].Status().Code == codes.Error {
		t.Error("a successful run was marked an error")
	}
}

// A failed migration is a failed span.
//
// Left unmarked, a trace of a Job that failed reads as one that succeeded --
// which is the wrong way round for the one thing somebody opens a trace of a
// migration to find out.
func TestAFailedRunIsAFailedSpan(t *testing.T) {
	rec := recorder(t)
	boom := errors.New("relation already exists")

	err := withSpan(context.Background(), "migrate.run", func(context.Context) error { return boom })

	if !errors.Is(err, boom) {
		t.Fatalf("the step's error was not returned unchanged: %v", err)
	}
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected one span, got %d", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Errorf("status = %v, want error", spans[0].Status().Code)
	}
	if len(spans[0].Events()) == 0 {
		t.Error("the error was not recorded on the span")
	}
}
