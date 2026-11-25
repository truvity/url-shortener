# Changelog

What changed for someone consuming this repository, newest first, one
heading per tag. The prose bullets are written for a consumer; the commit
subjects under them are the GitHub Release's own list.

## Unreleased

### Added

- **The route takes a list of parents: `exposure.parentRefs`.** A route that moves
  between gateways (the shared fleet Gateway to a per-vendor one, later an internal
  load balancer) with one parent leaves the old and joins the new in the same
  apply, and the gap is an outage. With a list it names both while traffic moves
  and one afterwards. It is read only when `route.parentRef` is not set, and has
  the same shape as `exposure.parentRefs` of the other charts of the platform
  (`name`, `namespace`, `sectionName`, `kind`, `group`); `group` and `kind` are
  always rendered (`gateway.networking.k8s.io`, `Gateway` when left out), so Argo CD
  sees no drift. `route.parentRef` is unchanged and renders the same bytes. The
  chart renders no ingress NetworkPolicy, so there is nothing else to admit.

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
