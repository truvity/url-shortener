# The runtime image for the url-shortener e2e verification suite.
#
# ONE stage, and no RUN line — service.md §7, the same convention
# ../log/Dockerfile, ../stat/Dockerfile and ../web/Dockerfile keep. Both
# things this image carries — the compiled test binary and kubectl — are
# built OUTSIDE it, by hack/build.sh; this only copies them in. Nothing
# executes while the image is assembled, so it is built for another
# architecture by a file copy, with no second runner and no emulation.
#
# The base is Chainguard's static image, the SAME one .ko.yaml's
# `defaultBaseImage` uses for this example's own Go binaries: the test
# binary is a `go test -c` build with CGO_ENABLED=0, statically linked the
# same way, and needs nothing from a libc. kubectl's own published binaries
# are static too, for the same reason `kos:` needs none here. Pinned by the
# same digest as .ko.yaml, and moved by renovate with it.
FROM cgr.dev/chainguard/static:latest@sha256:41e17ed83c594a64a9396b6ab96dd26d5ddc290dacf4c177464712ff21ad534f

ARG TARGETARCH
# The COPY path is from the REPOSITORY ROOT, not from this directory — the
# release stages this file into GoReleaser's build context keeping its
# path, and a local build runs the same configuration. One build of this
# image rather than two that can disagree.
COPY examples/url-shortener/e2e/build/e2e-suite-$TARGETARCH /usr/local/bin/e2e-suite
COPY examples/url-shortener/e2e/build/kubectl-$TARGETARCH /usr/local/bin/kubectl

USER 65532:65532

# `-test.v` so a verification Job's log carries every test's own name and
# result, not only a pass/fail summary line — the log IS the record of what
# a promotion was checked against.
ENTRYPOINT ["/usr/local/bin/e2e-suite"]
CMD ["-test.v"]
