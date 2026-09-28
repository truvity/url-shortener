// Package traceattrs checks a Jaeger-API trace against the span-attribute
// allow-list this example's four services are each configured with —
// proving, from OUTSIDE every process, that the claim every one of
// telemetry/attributes.go, ts/src/attributes.ts,
// python/src/truvity_policy/telemetry.py and
// examples/url-shortener/stat/.../SpanAttributeAllowlist.kt make on their
// own ("an attribute nobody allow-listed is exported") actually held for a
// real trace, not just for the unit tests each of those files carries
// beside it.
//
// It is a package of its own, not code living inside
// examples/url-shortener/e2e/suite alongside trace_test.go, for the same
// reason e2e/traceauth is: suite's TestMain (main_test.go) skips the WHOLE
// package unless E2E_NAMESPACE is set — the right behaviour for a test
// that needs a cluster, and the wrong one for what this package checks,
// which needs neither: a parsed trace's tags matching a table is a
// hermetic claim, provable with a JSON fixture on every `just check`,
// cluster or not (traceattrs_test.go).
package traceattrs

import (
	"fmt"
	"strings"

	policytelemetry "github.com/truvity/policy/telemetry"
)

// Tag is one Jaeger span tag: a key, and DELIBERATELY nothing else. Jaeger
// tags carry a value too, but this package never unmarshals it — a checker
// built on a type that cannot hold a value cannot leak one into a test's
// failure output by accident, which is worth more here than it would be
// almost anywhere else: a value is exactly the request data an allow-list
// exists to keep out of what a service exports.
type Tag struct {
	Key string `json:"key"`
}

// Span is the subset of Jaeger's per-span JSON this package reads: enough
// to say which service a span belongs to, what its tags' KEYS were (never
// their values — see Tag), and a name for a failure message. Timing, span
// and reference ids, logs — everything else — is out of scope for an
// allow-list check.
type Span struct {
	OperationName string `json:"operationName"`
	ProcessID     string `json:"processID"`
	Tags          []Tag  `json:"tags"`
}

// Process carries the Jaeger RESOURCE for one or more spans: service.name,
// and, in a real response, further process-level tags — k8s.*, host.*,
// telemetry.sdk.* and the rest of what OpenTelemetry calls resource
// attributes, describing the POD rather than the REQUEST. Those tags are
// deliberately never unmarshalled here (Jaeger's own JSON already keeps
// them on Process, separate from a span's own Tags), which is what keeps
// them out of scope for this package without it having to filter resource
// keys out by hand.
type Process struct {
	ServiceName string `json:"serviceName"`
}

// Trace is one Jaeger trace: its spans and the processes they belong to.
type Trace struct {
	Spans     []Span             `json:"spans"`
	Processes map[string]Process `json:"processes"`
}

// Response is the small subset of Jaeger's own JSON query API (GET
// {tracesURL}/api/traces/{traceID}) this package, and
// examples/url-shortener/e2e/suite's trace_test.go, both read — from the
// SAME fetch; see trace_test.go's own comment on why there is only one.
type Response struct {
	Data []Trace `json:"data"`
}

// Services returns the distinct service names that contributed a span to
// resp, in first-seen order.
func (resp Response) Services() []string {
	seen := map[string]bool{}
	var services []string
	for _, trace := range resp.Data {
		names := processNames(trace)
		for _, span := range trace.Spans {
			name := names[span.ProcessID]
			if name != "" && !seen[name] {
				seen[name] = true
				services = append(services, name)
			}
		}
	}
	return services
}

func processNames(trace Trace) map[string]string {
	names := make(map[string]string, len(trace.Processes))
	for id, p := range trace.Processes {
		names[id] = p.ServiceName
	}
	return names
}

// ServiceExtensions maps a url-shortener component name to the span
// attribute keys that component's own telemetry setup adds on top of
// policytelemetry.DefaultAllowedAttributes — matched against a span's
// process.serviceName by substring, the same way
// examples/url-shortener/e2e/suite/trace_test.go's own anyContains already
// matches service names, because charts/url-shortener/templates/_helpers.tpl's
// url-shortener.telemetryEnv renders OTEL_SERVICE_NAME as
// "<release>-<component>", never the bare component alone.
//
// Every entry names its own source below, because this example's four
// languages cannot share one Go-importable list:
//
//   - "urls", "redirect" (Go): cmd/urls/main.go and cmd/redirect/main.go
//     both call policytelemetry.WithAllowedAttributes("error") — the
//     boolean internal/api.Tracing sets on a failed request
//     (internal/api/tracing.go, `attribute.Bool("error", true)`).
//     TestGoServicesMatchServiceExtensions, in traceattrs_test.go, parses
//     both files' source and fails the moment the literal keys they pass
//     drift from what is written here. It reads the SOURCE rather than
//     importing a shared var because both files are `package main`, which
//     nothing outside them can import — the nearest either language gets
//     to one shared list without restructuring a live service's entry
//     point just to satisfy a test.
//   - "stat" (Kotlin): stat's own DEFAULT_ALLOWED_ATTRIBUTES
//     (stat/src/main/kotlin/com/truvity/example/stat/SpanAttributeAllowlist.kt),
//     UNCHANGED — Stat.kt sets only messaging.* keys, already in that
//     default set.
//   - "log" (Python): log/src/url_shortener_log/__main__.py calls
//     `telemetry.start(extra_attributes=("archive.records", "archive.key"))`.
//   - "web" (TS): web/src/server/main.ts calls `startTelemetry()` with no
//     `extraAttributes` — DEFAULT_ALLOWED_ATTRIBUTES unchanged (web's own
//     manually-created spans, in web/src/server/main.ts and urls.ts, set
//     only http.* and error status keys already on that default list).
//
// The four languages' DEFAULT lists are themselves identical — 22 keys,
// same order, checked by hand against telemetry/attributes.go,
// ts/src/attributes.ts, python/src/truvity_policy/telemetry.py and
// stat's SpanAttributeAllowlist.kt — so nothing here has to name a
// per-language default; policytelemetry.DefaultAllowedAttributes stands
// in for all four.
var ServiceExtensions = map[string][]string{
	"urls":     {"error"},
	"redirect": {"error"},
	"stat":     {},
	"log":      {"archive.records", "archive.key"},
	"web":      {},
}

// storeAddedTags are span tags a Jaeger-API-shaped trace store (or an OTel
// collector translating OTLP into that shape) writes onto a span itself —
// as opposed to onto the PROCESS it belongs to (Process, above — resource
// attributes, out of scope entirely) — that no service's own SDK ever put
// there.
//
// None of the four telemetry libraries' filtering exporters run
// downstream of this: every one of them wraps the SDK's own span
// EXPORTER (see telemetry/attributes.go's filteringExporter, and its
// Python/Kotlin/TS equivalents' identical doc comments on why the
// exporter, and not some later hook, is where the filter lives), and
// these tags are added AFTER that exporter has already handed the
// (already-filtered) span onward, by whatever turns OTLP into this JSON
// shape. A service's allow-list is not what let them through, so
// CheckAttributes does not blame a service for carrying them.
var storeAddedTags = map[string]string{
	"span.kind": "the OTLP SpanKind enum (SERVER, CLIENT, CONSUMER, " +
		"PRODUCER, INTERNAL) — Jaeger's tag-based span model has no " +
		"dedicated field for it, so the OTLP-to-Jaeger translation writes " +
		"it as a tag on every span",
	"otel.scope.name": "the span's InstrumentationScope name (the " +
		"tracer's own name) — the same translation attaches it as a tag " +
		"because Jaeger's model has no instrumentation-scope field either",
	"otel.scope.version": "the same InstrumentationScope's version, " +
		"attached alongside otel.scope.name for the same reason",
	"internal.span.format": "written by the trace store's own OTLP " +
		"ingestion to record which wire format a span arrived in — never " +
		"part of an OTLP span itself",
	"error": "derived from the OTLP span's Status.Code by the same " +
		"translation that synthesizes otel.status_code and " +
		"otel.status_description — both already " +
		"policytelemetry.DefaultAllowedAttributes keys, which is why " +
		"they need no entry here. Status is a field on a span distinct " +
		"from its Attributes, so every language's filtering exporter " +
		"(which touches only Attributes) never touches it, and this tag " +
		"reaches Jaeger even for a service that allow-lists no \"error\" " +
		"key of its own (stat, log, web, above)",
}

// Offense is one span attribute CheckAttributes found outside the
// allow-list for the service whose span carried it.
//
// It carries no VALUE, only the key — see Tag's own doc comment for why
// that is impossible to get wrong here rather than merely a convention:
// this struct has nowhere to put one.
type Offense struct {
	Service string
	Span    string
	Key     string
}

// String never prints a value — only the service, the span name, and the
// offending key.
func (o Offense) String() string {
	return fmt.Sprintf("service %q span %q attribute %q", o.Service, o.Span, o.Key)
}

// CheckAttributes reports every span attribute in resp that is outside
// BOTH the allow-list its own service is configured with (extensions,
// keyed and matched the way ServiceExtensions' own doc comment describes)
// AND storeAddedTags.
//
// A span whose service matches no key in extensions is skipped, not
// flagged: this package has no way to distinguish "a service this table
// deliberately says nothing about" from "a service nobody added a row
// for", and misattributing a span from neither case is the wrong failure
// to report from a function whose whole job is naming the right one.
func CheckAttributes(resp Response, extensions map[string][]string) []Offense {
	allowed := make(map[string]map[string]bool, len(extensions))
	for component, extra := range extensions {
		set := defaultAllowed()
		for _, key := range extra {
			set[key] = true
		}
		allowed[component] = set
	}

	var offenses []Offense
	for _, trace := range resp.Data {
		names := processNames(trace)
		for _, span := range trace.Spans {
			component, set := matchComponent(names[span.ProcessID], allowed)
			if set == nil {
				continue
			}
			for _, tag := range span.Tags {
				if set[tag.Key] {
					continue
				}
				if _, ok := storeAddedTags[tag.Key]; ok {
					continue
				}
				offenses = append(offenses, Offense{Service: component, Span: span.OperationName, Key: tag.Key})
			}
		}
	}
	return offenses
}

// defaultAllowed copies policytelemetry.DefaultAllowedAttributes into a
// set every per-service allow-list starts from — a copy, so extending one
// service's set in CheckAttributes can never leak into another's.
func defaultAllowed() map[string]bool {
	set := make(map[string]bool, len(policytelemetry.DefaultAllowedAttributes))
	for _, key := range policytelemetry.DefaultAllowedAttributes {
		set[string(key)] = true
	}
	return set
}

// matchComponent finds the one entry in allowed whose key is a substring
// of service, and reports it alongside its allow-list — see
// ServiceExtensions' own doc comment for why substring, not equality, is
// the right match here.
func matchComponent(service string, allowed map[string]map[string]bool) (string, map[string]bool) {
	for component, set := range allowed {
		if strings.Contains(service, component) {
			return component, set
		}
	}
	return "", nil
}
