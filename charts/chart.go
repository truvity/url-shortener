// Package charts carries the deployment for this example, embedded so that
// its tests cannot pass on a stale render.
//
// The embed is not decoration. The chart tests shell out to `helm`, and Go's
// test cache keys on the files a test package READS — not on what a
// subprocess reads. Without the embed, editing a template leaves the cached
// PASS in place and the contract goes unchecked for as long as nobody
// notices.
package charts

import "embed"

// Files is the chart, exactly as it is published.
//
//go:embed all:url-shortener
var Files embed.FS
