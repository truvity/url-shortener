// Package urlshortener carries this service's configuration schemas.
//
// It exists at the module root because `go:embed` cannot reach a parent
// directory: a package under internal/ cannot embed ../../schemas, so the
// embed lives where the files are and the config package reads through it.
package urlshortener

import "embed"

// Schemas are the schemas of this service's binaries. Embedded so that a
// binary validates against the schema it SHIPPED with, rather than one a
// deployment happens to have mounted beside it.
//
//go:embed schemas
var Schemas embed.FS
