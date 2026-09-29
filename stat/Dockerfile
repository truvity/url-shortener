# The runtime image for a JVM component.
#
# ONE stage, and no RUN line — service.md §7. The jar is built OUTSIDE the
# image by `just example-images`, and this copies it. There is nothing in
# here that can execute while the image is assembled, so it can be built for
# another architecture without emulation, and what is in it is a question
# with an answer: the jar, and a JRE.
#
# The base is pinned by digest as well as tag, the same as ../.ko.yaml and
# the other Dockerfiles here: `latest` alone is a different image on every
# build. The registry's free tier publishes only `latest` but keeps old
# digests resolvable (see ../log/Dockerfile); renovate moves the pin. What
# must not move under a pin bump is the bytecode LEVEL, and that is pinned
# too — the Gradle toolchain targets 21, and a JRE newer than that runs it.
FROM cgr.dev/chainguard/jre:latest@sha256:d1ea31930e42338d67a617781f2ad3a181c10645acc8e8653853e37a91b79fa5

# Already unprivileged in the base; stated here and in the chart where a
# reader and an admission policy can both see it.
USER 65532:65532

# The COPY path is from the REPOSITORY ROOT, not from this directory, and
# that is deliberate: the release stages this file into GoReleaser's build
# context keeping its path, and the local build runs the same
# configuration. One build of this image rather than two that can disagree.
#
COPY examples/url-shortener/stat/build/libs/stat.jar /app/stat.jar

ENTRYPOINT ["java", "-jar", "/app/stat.jar"]
