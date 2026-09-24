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

// Files is BOTH charts, exactly as they are published.
//
// Both, because the split between them is itself part of the contract — the
// application chart must not create what its migration migrates — and a
// test that only ever rendered one half could not see that hold.
//
//go:embed all:url-shortener all:url-shortener-infra
var Files embed.FS
