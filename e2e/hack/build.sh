#!/usr/bin/env bash
#
# Assemble what the e2e verification image copies, OUTSIDE the image, for
# every architecture the image is published for — the same shape
# ../../log/hack/build.sh uses for the Python component, for the same
# reason: RUN-less service.md §7 needs everything already built before the
# Dockerfile's one COPY.
#
# Two things land per architecture:
#
#   - the compiled test binary (`go test -c`) for
#     examples/url-shortener/e2e/suite — cross-compiled the same way the
#     `kos` entries in .goreleaser.yaml cross-compile the application's own
#     binaries: CGO_ENABLED=0, GOOS=linux, GOARCH=$arch, no emulation;
#   - a pinned `kubectl`, because the suite's own db_test.go and
#     env_test.go shell out to it (reading a Secret, and every Deployment's
#     rollout status) — see docs/guides/testing.md. The application's own
#     images need no such thing; this is the one image in this repository
#     that talks to the Kubernetes API rather than only to Services on it.
set -euo pipefail

cd "$(dirname "$0")/.."

# Architectures to build, as Docker (and Go's GOARCH) names them — the
# default is every one a release publishes for, matching every other
# component; ARCHES is the override for a fast local loop.
ARCHES=${ARCHES:-amd64 arm64}

# kubectl's own release, pinned by version AND verified by the checksum
# dl.k8s.io publishes beside every binary — a tag that moved under a cached
# pull should not become a silent supply-chain surprise in an image that
# talks to the cluster's API. Kept near kind's own NODE_IMAGE
# (../../hack/kind/versions.env) rather than copied from it: this image
# runs against whatever cluster promoted it, not only the kind box, and
# kubectl's skew policy (±1 minor) covers the difference.
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.34.0}

rm -rf build
mkdir -p build

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }

step "the e2e suite's test binary, for $ARCHES"
for arch in $ARCHES; do
    out="build/e2e-suite-$arch"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
        go test -c -o "$out" -trimpath -ldflags="-s -w" ./suite
    echo "built $out"
done

step "kubectl $KUBECTL_VERSION, for $ARCHES"
for arch in $ARCHES; do
    bin="build/kubectl-$arch"
    url="https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl"

    curl --fail --silent --show-error --location --output "$bin" "$url"
    curl --fail --silent --show-error --location --output "$bin.sha256" "$url.sha256"

    (cd build && echo "$(cat "kubectl-$arch.sha256")  kubectl-$arch" | sha256sum --check --status)
    rm -f "$bin.sha256"
    chmod +x "$bin"
    echo "verified $bin"
done
