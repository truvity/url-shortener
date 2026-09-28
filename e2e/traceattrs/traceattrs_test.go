package traceattrs

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// parse decodes a fixture Jaeger trace response — the same JSON shape
// GET {tracesURL}/api/traces/{traceID} answers with, trimmed to the
// fields Response reads.
func parse(t *testing.T, raw string) Response {
	t.Helper()
	var resp Response
	require.NoError(t, json.Unmarshal([]byte(raw), &resp))
	return resp
}

// allowedOnlyTrace carries one span each for redirect and stat: only
// keys on their own allow-list (policytelemetry.DefaultAllowedAttributes
// plus, for redirect, its "error" extension), PLUS one of every
// storeAddedTags key, proving those are exempted rather than merely
// absent from this fixture.
const allowedOnlyTrace = `{
  "data": [
    {
      "processes": {
        "p1": {"serviceName": "url-shortener-redirect"},
        "p2": {"serviceName": "url-shortener-stat"}
      },
      "spans": [
        {
          "operationName": "GET /r/{key}",
          "processID": "p1",
          "tags": [
            {"key": "http.request.method", "value": "GET"},
            {"key": "http.route", "value": "/r/{key}"},
            {"key": "http.response.status_code", "value": 200},
            {"key": "error", "value": false},
            {"key": "span.kind", "value": "server"},
            {"key": "otel.scope.name", "value": "github.com/truvity/policy/examples/url-shortener"},
            {"key": "otel.scope.version", "value": "1.0.0"},
            {"key": "internal.span.format", "value": "otlp"}
          ]
        },
        {
          "operationName": "url-shortener.v1.URLRedirect receive",
          "processID": "p2",
          "tags": [
            {"key": "messaging.system", "value": "nats"},
            {"key": "messaging.destination.name", "value": "url-shortener.redirect"},
            {"key": "messaging.operation.type", "value": "receive"},
            {"key": "otel.status_code", "value": "OK"}
          ]
        }
      ]
    }
  ]
}`

func TestCheckAttributesAllowsAnAllowedOnlyTrace(t *testing.T) {
	offenses := CheckAttributes(parse(t, allowedOnlyTrace), ServiceExtensions)
	require.Empty(t, offenses, "%v", offenses)
}

// unlistedAttributeTrace gives stat's span one key — url.path — that is
// on neither its allow-list nor storeAddedTags: exactly the request data
// (here, deliberately, a value that would identify a real user if this
// were live) an allow-list exists to keep out.
const unlistedAttributeTrace = `{
  "data": [
    {
      "processes": {
        "p1": {"serviceName": "url-shortener-stat"}
      },
      "spans": [
        {
          "operationName": "url-shortener.v1.URLRedirect receive",
          "processID": "p1",
          "tags": [
            {"key": "messaging.system", "value": "nats"},
            {"key": "url.path", "value": "/r/abc123-do-not-print-me"}
          ]
        }
      ]
    }
  ]
}`

func TestCheckAttributesFlagsAnUnlistedAttribute(t *testing.T) {
	offenses := CheckAttributes(parse(t, unlistedAttributeTrace), ServiceExtensions)
	require.Len(t, offenses, 1)

	got := offenses[0]
	require.Equal(t, "stat", got.Service)
	require.Equal(t, "url-shortener.v1.URLRedirect receive", got.Span)
	require.Equal(t, "url.path", got.Key)

	// The failure message names the key, never the value — this is the
	// property the whole package exists for, so it is asserted on the
	// exact string a caller would print, not just on the Offense struct.
	msg := got.String()
	require.Contains(t, msg, "url.path")
	require.NotContains(t, msg, "do-not-print-me",
		"Offense.String must never leak an attribute VALUE")
}

// unknownServiceTrace carries a span from a process this example's own
// components never produce (some sidecar or the collector itself), with a
// tag that would be an offense on any of url-shortener's own services.
// CheckAttributes must skip it rather than misattribute it to whichever
// component name happens to substring-match.
const unknownServiceTrace = `{
  "data": [
    {
      "processes": {
        "p1": {"serviceName": "some-other-collector"}
      },
      "spans": [
        {
          "operationName": "export",
          "processID": "p1",
          "tags": [
            {"key": "url.path", "value": "/should/not/be/flagged"}
          ]
        }
      ]
    }
  ]
}`

func TestCheckAttributesSkipsAServiceWithNoTableEntry(t *testing.T) {
	offenses := CheckAttributes(parse(t, unknownServiceTrace), ServiceExtensions)
	require.Empty(t, offenses, "%v", offenses)
}

func TestCheckAttributesUsesThePerServiceExtension(t *testing.T) {
	// log's own extension ("archive.records", "archive.key") must survive
	// on log's span and must NOT be granted to a service that never
	// declared it.
	trace := `{
	  "data": [
	    {
	      "processes": {
	        "p1": {"serviceName": "url-shortener-log"},
	        "p2": {"serviceName": "url-shortener-web"}
	      },
	      "spans": [
	        {
	          "operationName": "url-shortener.v1.URLRequest receive",
	          "processID": "p1",
	          "tags": [{"key": "archive.key", "value": "2026/09/28/00001.ndjson"}]
	        },
	        {
	          "operationName": "GET /r/{key}",
	          "processID": "p2",
	          "tags": [{"key": "archive.key", "value": "should-not-be-web-s"}]
	        }
	      ]
	    }
	  ]
	}`

	offenses := CheckAttributes(parse(t, trace), ServiceExtensions)
	require.Len(t, offenses, 1)
	require.Equal(t, "web", offenses[0].Service)
	require.Equal(t, "archive.key", offenses[0].Key)
}

// TestGoServicesMatchServiceExtensions parses cmd/urls/main.go and
// cmd/redirect/main.go — the two Go services in ServiceExtensions — and
// fails the moment the literal keys either passes to
// policytelemetry.WithAllowedAttributes drift from what ServiceExtensions
// says. See ServiceExtensions' own doc comment for why reading the
// SOURCE, rather than importing a shared var, is what a `package main`
// callee allows here.
func TestGoServicesMatchServiceExtensions(t *testing.T) {
	for _, tc := range []struct {
		component string
		file      string
	}{
		{"urls", "../../cmd/urls/main.go"},
		{"redirect", "../../cmd/redirect/main.go"},
	} {
		t.Run(tc.component, func(t *testing.T) {
			got, err := withAllowedAttributesArgs(tc.file)
			require.NoError(t, err)

			want := append([]string{}, ServiceExtensions[tc.component]...)
			sort.Strings(got)
			sort.Strings(want)
			require.Equal(t, want, got,
				"%s's own WithAllowedAttributes call no longer matches traceattrs.ServiceExtensions[%q] — update whichever one is stale",
				tc.file, tc.component)
		})
	}
}

// withAllowedAttributesArgs parses the Go source at path and returns the
// string literal arguments of its one
// policytelemetry.WithAllowedAttributes(...) call, in source order.
//
// AST, not a regular expression: a call this reads through go/parser
// cannot be matched by a comment that happens to contain the same text —
// see the false positive this would otherwise risk against this very
// package's own doc comments, which quote the call in full.
func withAllowedAttributesArgs(path string) ([]string, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, err
	}

	var args []string
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "WithAllowedAttributes" {
			return true
		}
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, err := strconv.Unquote(lit.Value)
			if err == nil {
				args = append(args, value)
			}
		}
		return true
	})
	return args, nil
}
