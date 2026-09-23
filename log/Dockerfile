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
# The base is Chainguard's Python by tag rather than by digest, which is the
# same choice ../.ko.yaml makes for the Go components. The free tier keeps
# only the current build, so a pinned digest here would stop resolving and
# break every fork's build with a failure whose cause is invisible. The
# interpreter's MINOR version is what matters and it is pinned where it can
# be: devbox.json declares the one that resolves the lock and builds the
# wheels, and a mismatch fails the build rather than the deployment.
FROM cgr.dev/chainguard/python:latest

# Installed, not built. See above.
COPY build/site-packages /app

ENV PYTHONPATH=/app \
    PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

# The image's user is already unprivileged, and the chart states the same
# numbers where an admission policy can see them.
USER 65532:65532

ENTRYPOINT ["python", "-m", "url_shortener_log"]
