rootProject.name = "stat"

// The loader is the checkout, the way the Go module uses `replace` and the
// Python component uses a path dependency. There is no published artifact
// to resolve instead: no release ships the Kotlin loader (kotlin/README.md,
// "Status"), so a consumer outside this repository builds it from a
// checkout of the tag it pins.
includeBuild("../../../kotlin")
