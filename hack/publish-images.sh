#!/usr/bin/env bash
#
# Publish every component's image, for every architecture, in one run and
# with no emulation anywhere.
#
# That last part is the whole reason the images are shaped the way they are.
# Not one of the six Dockerfiles has a RUN line: the Go binaries are
# cross-compiled by ko, the Python dependency trees are resolved per
# architecture by uv, and the jar and the JavaScript bundle are the same
# bytes everywhere. So building for arm64 on an x86 machine copies files
# rather than executing them, and needs no second runner and no qemu.
#
# It is not academic: the cluster this example is deployed to runs arm64
# nodes, and the local box is x86. An image built only for the host would
# install and then never start.
#
# Prints one `component=digest` line per image, which is what a caller pins.
set -euo pipefail

cd "$(dirname "$0")/.."

REPOSITORY=${REPOSITORY:?set REPOSITORY, e.g. ghcr.io/truvity/policy/url-shortener}
VERSION=${VERSION:?set VERSION, e.g. 0.2.0}
PLATFORMS=${PLATFORMS:-linux/amd64,linux/arm64}
ARCHES=${ARCHES:-amd64 arm64}
BUILDER=${BUILDER:-policy-multiarch}

# A container-driver builder, because the default one cannot produce an image
# for a platform it is not running on even when nothing in the build
# executes.
docker buildx inspect "$BUILDER" >/dev/null 2>&1 ||
    docker buildx create --name "$BUILDER" --driver docker-container >/dev/null

digest_of() {
    docker buildx imagetools inspect "$1" --format '{{json .Manifest}}' 2>/dev/null |
        python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])'
}

echo "==> the Go components" >&2
export KO_DOCKER_REPO="$REPOSITORY"
for component in migrate redirect urls; do
    ko build -B --platform "$PLATFORMS" --tags "$VERSION" "./cmd/$component" >/dev/null
    echo "$component=$(digest_of "$REPOSITORY/$component:$VERSION")"
done

echo "==> the Python component" >&2
ARCHES="$ARCHES" bash log/hack/build.sh >&2
docker buildx build --builder "$BUILDER" --platform "$PLATFORMS" \
    --tag "$REPOSITORY/log:$VERSION" --push log >&2
echo "log=$(digest_of "$REPOSITORY/log:$VERSION")"

echo "==> the Kotlin component" >&2
( cd stat && gradle bootJar --console=plain --quiet ) >&2
docker buildx build --builder "$BUILDER" --platform "$PLATFORMS" \
    --tag "$REPOSITORY/stat:$VERSION" --push stat >&2
echo "stat=$(digest_of "$REPOSITORY/stat:$VERSION")"

echo "==> the TypeScript component" >&2
( cd ../../ts && yarn install --immutable && yarn build ) >&2
( cd web && yarn install --immutable && yarn build ) >&2
docker buildx build --builder "$BUILDER" --platform "$PLATFORMS" \
    --tag "$REPOSITORY/web:$VERSION" --push web >&2
echo "web=$(digest_of "$REPOSITORY/web:$VERSION")"
