# The runtime image for a Python component.
#
# ONE stage, and no RUN line — service.md §7, kept honestly rather than by
# hiding the build in an earlier stage. Everything this image contains was
# produced outside it, by `just example-images`: the dependency tree is
# resolved from the committed lock and installed into a directory, the two
# first-party wheels are built and installed beside it, and the result is
# copied in.
#
# Two consequences follow, and both are the point. There is nothing in here
# that can execute at build time, so the image can be assembled for another
# architecture without emulation. And what is in it is a question with an
# answer: the lock.
#
# The base is Chainguard's Python, pinned by DIGEST as well as tag, the same
# as ../.ko.yaml and the other Dockerfiles here: `latest` alone is a
# different image on every build, so two builds of one commit could ship two
# interpreters. The free tier publishes only `latest`, but it keeps the old
# digests resolvable (measured 2026-09-29: digests from 2022 still resolve),
# so the pin holds; renovate's docker manager moves it in a pull request.
# The interpreter's MINOR version is what matters, and devbox.json declares
# the one that resolves the lock and builds the wheels — a mismatch fails
# the build rather than the deployment.
FROM cgr.dev/chainguard/python:latest@sha256:a1775c7276078865461ee5714954284f12809f333433d856d720b249c65c11b2

# Installed, not built. See above.
#
# TARGETARCH is set by the builder for each platform it is producing, and the
# build script laid down one tree per architecture — so a multi-architecture
# image is assembled by COPYING the right one, with nothing executing and no
# emulation anywhere.
#
# A release publishes every architecture, always: a published artifact is
# consumed by machines whose architecture the publisher does not know. A
# local build is host-only because it loads into one node. See
# hack/publish-images.sh.
ARG TARGETARCH
# The COPY path is from the REPOSITORY ROOT, not from this directory, and
# that is deliberate: the release stages this file into GoReleaser's build
# context keeping its path, and the local build runs the same
# configuration. One build of this image rather than two that can disagree.
#
COPY examples/url-shortener/log/build/site-packages-$TARGETARCH /app

ENV PYTHONPATH=/app \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# The image's user is already unprivileged, and the chart states the same
# numbers where an admission policy can see them.
USER 65532:65532

ENTRYPOINT ["python", "-m", "url_shortener_log"]
