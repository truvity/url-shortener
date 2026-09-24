rootProject.name = "stat"

// The loader is the checkout, the way the Go module uses `replace` and the
// Python component uses a path dependency. A consumer outside this
// repository resolves the published artifact instead.
includeBuild("../../../kotlin")
