#!/usr/bin/env bash
#
# Assemble what the runtime image copies, OUTSIDE the image, for every
# architecture the image is published for.
#
# The Go components need none of this: ko cross-compiles a static binary and
# lays an image around it. Python has no equivalent, so this script is the
# equivalent — resolve from the committed lock, install into a directory per
# architecture, and leave an image with nothing to do but copy.
#
# **Cross-architecture without emulation**, which is the property the service
# contract's "one job builds every platform" is really about. `uv` resolves
# wheels for a platform it is not running on, so an arm64 tree is built on an
# x86 machine by downloading arm64 wheels — no qemu, no second runner, and
# the native extensions really are arm64:
#
#     rpds.cpython-314-aarch64-linux-gnu.so: ELF 64-bit LSB shared object, ARM aarch64
#
# `--only-binary=:all:` is what keeps that true. Without it a dependency with
# no wheel for the target would be built from source — on the HOST's
# architecture, silently, producing a tree that imports on the build machine
# and crashes in the cluster.
set -euo pipefail

cd "$(dirname "$0")/.."

# Architectures to build, as Docker names them. The default is the host's,
# because a local run wants one tree and wants it quickly.
# EVERY architecture by default, because this tree is what the release's
# image copies in and a release is always multi-architecture. The default
# used to be the host's, which is correct for a fast local iteration and
# silently wrong for everything else: the release built one tree and then
# failed copying the other, which is the good outcome -- the bad one is a
# default that quietly produces half a release.
#
# ARCHES is still the override, for exactly that fast local iteration.
ARCHES=${ARCHES:-amd64 arm64}

rm -rf build
mkdir -p build/dist

# Third-party dependencies, exactly as the lock resolves them. `--no-dev`
# because a test runner in a runtime image is a larger attack surface for no
# reason, and `--no-emit-project` plus the first-party name because those are
# built from the checkout below rather than downloaded.
uv export --quiet --frozen --no-dev --no-emit-project --no-hashes \
    --no-emit-package truvity-policy \
    --format requirements.txt \
    --output-file build/requirements.txt

# The two first-party wheels: the shared loader and this component. Pure
# Python, so one build serves every architecture.
uv build --quiet --wheel --out-dir build/dist ../../../python
uv build --quiet --wheel --out-dir build/dist .

for arch in $ARCHES; do
    case "$arch" in
        amd64) platform=x86_64-manylinux_2_28 ;;
        arm64) platform=aarch64-manylinux_2_28 ;;
        *) echo "build: no wheel platform known for $arch" >&2; exit 1 ;;
    esac

    out="build/site-packages-$arch"
    mkdir -p "$out"

    uv pip install --quiet --target "$out" \
        --python-platform "$platform" --only-binary=:all: \
        --requirement build/requirements.txt
    uv pip install --quiet --target "$out" \
        --python-platform "$platform" --only-binary=:all: \
        --no-deps build/dist/*.whl

    # The component itself, not just its dependencies. A tree that resolved
    # but did not install the first-party wheels starts and then cannot
    # import anything, which is a container crash a long way from here.
    for package in truvity_policy url_shortener_log; do
        if [ ! -d "$out/$package" ]; then
            echo "build: $package is not in $out" >&2
            exit 1
        fi
    done

    echo "built $(find "$out" -maxdepth 1 -name '*.dist-info' | wc -l) distributions for $arch"
done
