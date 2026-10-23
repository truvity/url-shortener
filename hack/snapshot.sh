#!/usr/bin/env bash
#
# Build the images and package the charts EXACTLY the way a release does,
# for the integration run: GoReleaser builds and pushes every image and
# records what it pushed, then helmctl reads that and bakes the digests into
# the charts (.goreleaser.yaml's own header says why they are one job).
#
# What is different here, and only here:
#
#   - the tag is ci-<sha>, not a version, and no GitHub release is cut (the
#     two environment variables the release configuration's header names);
#   - the images go to the repository's own ghcr.io namespace, the same
#     packages a release publishes to. They are public, because the cluster
#     pulls them anonymously by digest; the charts are packaged to
#     dist/charts and installed from there, never pushed.
#
# It needs a registry login (`docker login ghcr.io`, packages: write) and a
# buildx builder that can push a multi-platform image (the docker-container
# driver). Both are the workflow's; a laptop runs the same script with its own.
#
# NEVER `--snapshot`: it is the one GoReleaser mode that also stops the images
# being pushed, which is exactly the one thing this needs. `--skip=validate`
# is what makes a commit that is not a tag releasable instead.
set -euo pipefail
cd "$(dirname "$0")/.."

export IMAGE_REPO="${IMAGE_REPO:-ghcr.io/truvity/url-shortener}"
export KO_DOCKER_REPO="$IMAGE_REPO"
export IMAGE_TAG="${IMAGE_TAG:-ci-$(git rev-parse --short=12 HEAD)}"
export GITHUB_RELEASE_DISABLED=true
# GoReleaser refuses a repository with no tags (this one had none until its
# first release, and a shallow checkout has none). It never creates this tag:
# it only needs a version to stamp, and `--skip=validate` below lets it differ
# from HEAD. A real tag, when HEAD has one, still wins.
export GORELEASER_CURRENT_TAG="${GORELEASER_CURRENT_TAG:-v0.0.0-${IMAGE_TAG}}"

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }

step "the images, built and pushed to ${IMAGE_REPO} as ${IMAGE_TAG}"
goreleaser release --clean --skip=validate,announce

step "the charts, packaged from what was just pushed"
rm -rf dist/charts
bin/helmctl goreleaser-manifest --goreleaser-dist dist -o dist/goreleaser-manifest.json
for chart in url-shortener url-shortener-infra; do
    bin/helmctl package \
        --chart "charts/$chart" \
        --manifest dist/goreleaser-manifest.json \
        --require-image-digests \
        --output dist/charts
done

ls dist/charts
