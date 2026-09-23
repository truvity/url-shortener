#!/usr/bin/env bash
#
# Assemble what the runtime image copies, OUTSIDE the image.
#
# The Go components need none of this: ko cross-compiles a static binary and
# lays an image around it. Python has no equivalent, so this script is the
# equivalent — resolve from the committed lock, install into a directory,
# and leave an image with nothing to do but copy.
set -euo pipefail

cd "$(dirname "$0")/.."
out="build/site-packages"
rm -rf build
mkdir -p "$out" build/dist

# Third-party dependencies, exactly as the lock resolves them. `--no-dev`
# because a test runner in a runtime image is a larger attack surface for no
# reason, and `--no-emit-project` plus the two first-party names because
# those are built from the checkout below rather than downloaded.
uv export --quiet --frozen --no-dev --no-emit-project --no-hashes \
    --no-emit-package truvity-policy \
    --format requirements.txt \
    --output-file build/requirements.txt

uv pip install --quiet --target "$out" --requirement build/requirements.txt

# The two first-party wheels: the shared loader and this component.
uv build --quiet --wheel --out-dir build/dist ../../../python
uv build --quiet --wheel --out-dir build/dist .
uv pip install --quiet --target "$out" --no-deps build/dist/*.whl

# A build that silently produced an environment for the wrong interpreter is
# a container that starts and then cannot import anything. The base image's
# minor version and devbox's are the same on purpose; say so if they drift.
installed=$(find "$out" -maxdepth 1 -name '*.dist-info' | wc -l)
if [ "$installed" -eq 0 ]; then
    echo "build: nothing was installed into $out" >&2
    exit 1
fi

# The component itself, not just its dependencies. A tree that resolved but
# did not install the first-party wheels starts and then cannot import
# anything, which is a container crash a long way from this script.
for package in truvity_policy url_shortener_log; do
    if [ ! -d "$out/$package" ]; then
        echo "build: $package is not in $out" >&2
        exit 1
    fi
done

echo "built $installed distributions into $out"
