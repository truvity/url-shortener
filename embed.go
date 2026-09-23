// Package urlshortener carries this service's configuration schemas.
//
// It exists at the module root because `go:embed` cannot reach a parent
// directory: a package under internal/ cannot embed ../../schemas, so the
// embed lives where the files are and the config package reads through it.
package urlshortener

import "embed"

// Schemas are the schemas of this service's Go binaries. Embedded so that a
// binary validates against the schema it SHIPPED with, rather than one a
// deployment happens to have mounted beside it.
//
//go:embed schemas
var Schemas embed.FS

// PythonSchemas are the schemas of the components that are not Go.
//
// They live beside their own code rather than in schemas/, because a Python
// wheel carries only what is inside the package: a schema a directory above
// it would be present in a checkout and missing from the image, and every
// test would still pass. See log/src/url_shortener_log/README.md.
//
// They are embedded HERE anyway, and that is the point of this variable. The
// chart's tests validate what the chart renders against the schema the
// binary reads, and Go's test cache keys on the files the test package
// reads — not on what a helper opened at run time. A test that read this
// file from disk would keep a stale PASS after the schema changed, which is
// the one failure these tests exist to prevent.
//
//go:embed log/src/url_shortener_log/log.schema.json
var PythonSchemas embed.FS
