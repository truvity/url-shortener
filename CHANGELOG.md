# Changelog

What changed for someone consuming this repository, newest first, one
heading per tag. The prose bullets are written for a consumer; the commit
subjects under them are the GitHub Release's own list.

## Unreleased

## v1.55.0

### Changed

- **The repository moved.** The worked example was `examples/url-shortener` in
  truvity/policy until v1.54.2 (2026-10-22) and is now this repository, with
  its history (tags dropped). The Go module path is
  `github.com/truvity/url-shortener`. Nothing imported the old one.
- **The charts and images are published from here.** `url-shortener` and
  `url-shortener-infra` go to the same packages as before
  (`ghcr.io/truvity/charts/url-shortener` and `…/url-shortener-infra`), so a
  consumer's version range is unchanged and this is the next version after
  policy's last. The images move to `ghcr.io/truvity/url-shortener/<component>`;
  the images of earlier chart versions stay where they were, pinned by digest.
- **The library chart is no longer in this tree.** `service-lib` is truvity/policy's
  and the charts depend on its published archive at the version of the policy
  Go module in `go.mod`.
- **The TypeScript, Python and Kotlin configuration loaders moved with it and
  are no longer published** (no npm package, no wheel). The components here
  consume them in place; their fixtures and shared schemas come from the policy
  module at the pinned version.

### Added

- **An integration suite that uses the devel cluster through two doors**: the
  Kubernetes API and the release's public host, with no access to the database,
  the broker or the bucket from the test process. It replaces the suite that ran
  against a local kind cluster. The migration and the two database roles are
  tested against a local PostgreSQL (`internal/migration`). See
  `docs/integration-testing.md`.
- The workflows run on GitHub-hosted runners only, and a pull request from a
  fork runs the plain check alone.

### Removed

- The kind-box lane and its fixture, the in-cluster `e2e` image, the trace-store
  test (a third door) and the transport-identity helper that only the in-cluster
  suite used.
