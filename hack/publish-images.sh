#!/usr/bin/env bash
#
# Publish every component's image, for every architecture, in one run and
# with no emulation anywhere.
#
# A RELEASE IS ALWAYS MULTI-ARCHITECTURE. Not because of what any particular
# cluster runs today — that is a fact about this week — but because a
# published artifact is consumed by machines whose architecture the
# publisher does not know and should not have to ask about. An image that
# carries one architecture is an image that fails for half its consumers,
# and it fails at `kubectl apply` time in somebody else's cluster, which is
# the worst possible place to learn it.
#
# The service contract has said this from the beginning (§7, "one job builds
# every platform"). This script is where it is enforced rather than hoped
# for: the platform list is asserted against the manifest that was actually
# produced, below, so a release cannot quietly ship less than it promised.
#
# It costs nothing, and that is the point of the image shape. Not one of the
# six Dockerfiles has a RUN line: the Go binaries are cross-compiled by ko,
# the Python dependency trees are resolved per architecture by uv, and the
# jar and the JavaScript bundle are the same bytes everywhere. So building
# for arm64 on an x86 machine copies files rather than executing them, and
# needs no second runner and no qemu.
#
# A LOCAL build is host-only, and that is a different question rather than a
# relaxation of this one: the local box loads images into one node of one
# architecture, so the second is waste. See `just example-images`.
#
# Prints one `component=digest` line per image, which is what a caller pins.
#
# `DRY_RUN=1` builds every image for every architecture and pushes nothing,
# so the whole path can be run before a tag rather than by one. Release
# machinery that can only be tested by tagging is tested the one way that
# costs a version.
set -euo pipefail

cd "$(dirname "$0")/.."

REPOSITORY=${REPOSITORY:?set REPOSITORY, e.g. ghcr.io/truvity/policy/url-shortener}
VERSION=${VERSION:?set VERSION, e.g. 0.2.0}
# Every architecture a release carries. Deliberately NOT overridable: a
# release that can be told to publish one architecture is a release that
# eventually will be, by a caller in a hurry, and the result is
# indistinguishable from a correct one until somebody else pulls it.
PLATFORMS="linux/amd64,linux/arm64"
ARCHES="amd64 arm64"
BUILDER=${BUILDER:-policy-multiarch}
DRY_RUN=${DRY_RUN:-}

# What a build does with its result: push it, or keep it as a local OCI
# layout and report a placeholder digest.
if [ -n "$DRY_RUN" ]; then
    out="$(mktemp -d)"
    push() { echo "--output" "type=oci,dest=$out/$1.tar"; }
    ko_push=--push=false
    digest_of() { echo "dry-run"; }
else
    push() { echo "--push"; }
    ko_push=--push=true
fi

# A container-driver builder, because the default one cannot produce an image
# for a platform it is not running on even when nothing in the build
# executes.
docker buildx inspect "$BUILDER" >/dev/null 2>&1 ||
    docker buildx create --name "$BUILDER" --driver docker-container >/dev/null

if [ -z "$DRY_RUN" ]; then
    digest_of() {
        docker buildx imagetools inspect "$1" --format '{{json .Manifest}}' 2>/dev/null |
            python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])'
    }
fi

# What the manifest actually says, as a sorted platform list. An image index
# that lost an architecture is the failure this guards: everything succeeds,
# the digest is real, and the image is wrong for half the estate.
assert_platforms() {
    local reference=$1 want got
    [ -n "$DRY_RUN" ] && return 0

    want=$(printf '%s\n' "${PLATFORMS//,/$'\n'}" | sort | tr '\n' ' ')
    got=$(docker buildx imagetools inspect "$reference" --raw |
        python3 -c '
import json, sys
index = json.load(sys.stdin)
out = []
for manifest in index.get("manifests", []):
    platform = manifest.get("platform", {})
    if platform.get("os") in (None, "unknown"):
        continue
    out.append(platform["os"] + "/" + platform["architecture"])
print(" ".join(sorted(out)) + " ")')

    if [ "$want" != "$got" ]; then
        echo "publish: $reference carries [$got], expected [$want]" >&2
        echo "         a release that ships one architecture fails for half its consumers" >&2
        exit 1
    fi
}

echo "==> the Go components" >&2
export KO_DOCKER_REPO="$REPOSITORY"
for component in migrate redirect urls; do
    ko build -B "$ko_push" --platform "$PLATFORMS" --tags "$VERSION" "./cmd/$component" >/dev/null
    assert_platforms "$REPOSITORY/$component:$VERSION"
    echo "$component=$(digest_of "$REPOSITORY/$component:$VERSION")"
done

echo "==> the Python component" >&2
ARCHES="$ARCHES" bash log/hack/build.sh >&2
docker buildx build --builder "$BUILDER" --platform "$PLATFORMS" \
    --tag "$REPOSITORY/log:$VERSION" $(push log) log >&2
assert_platforms "$REPOSITORY/log:$VERSION"
echo "log=$(digest_of "$REPOSITORY/log:$VERSION")"

echo "==> the Kotlin component" >&2
( cd stat && gradle bootJar --console=plain --quiet ) >&2
docker buildx build --builder "$BUILDER" --platform "$PLATFORMS" \
    --tag "$REPOSITORY/stat:$VERSION" $(push stat) stat >&2
assert_platforms "$REPOSITORY/stat:$VERSION"
echo "stat=$(digest_of "$REPOSITORY/stat:$VERSION")"

echo "==> the TypeScript component" >&2
( cd ../../ts && yarn install --immutable && yarn build ) >&2
( cd web && yarn install --immutable && yarn build ) >&2
docker buildx build --builder "$BUILDER" --platform "$PLATFORMS" \
    --tag "$REPOSITORY/web:$VERSION" $(push web) web >&2
assert_platforms "$REPOSITORY/web:$VERSION"
echo "web=$(digest_of "$REPOSITORY/web:$VERSION")"
