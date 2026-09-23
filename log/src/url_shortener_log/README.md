The schema beside the code is deliberate, and it is the same decision the Go
components make for the opposite reason.

Go's schemas sit at the module root in `../../schemas/` because `go:embed`
cannot reach a parent directory, so the embed has to live where the files
are. A Python wheel has the mirror-image constraint: only what is inside the
package is carried, so a schema a directory above would be present in a
checkout and missing from the image — which is the worst way to find out,
because every test passes.

So each language carries its component's schema where its packaging can
reach it, and there is exactly one copy of each. The chart's tests read this
file by path, the same way they read the others.
