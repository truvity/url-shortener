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

// Files is both charts of the product, exactly as they are published, and the
// example chart that follows the library convention.
//
// Both, because the split between them is itself part of the contract: the
// application chart must not create what its migration migrates, and a test
// that only ever rendered a subset could not see that hold.
//
// The library chart is not here: it is at the repository root, charts/service-lib,
// and embedded by package github.com/truvity/policy/charts. The two charts that
// depend on it resolve it with `helm dependency build`. `testdata/service-example`
// is the smallest chart that follows the library convention, which the library's
// own tests render.
//
//go:embed all:url-shortener all:url-shortener-infra all:testdata/service-example
var Files embed.FS
