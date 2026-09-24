# The runtime image for a JVM component.
#
# ONE stage, and no RUN line — service.md §7. The jar is built OUTSIDE the
# image by `just example-images`, and this copies it. There is nothing in
# here that can execute while the image is assembled, so it can be built for
# another architecture without emulation, and what is in it is a question
# with an answer: the jar, and a JRE.
#
# The base is by tag rather than by digest, which is the same choice
# ../.ko.yaml makes for the Go components and log/Dockerfile makes for the
# Python one: the registry's free tier keeps only the current build, so a
# pinned digest stops resolving and breaks every fork with a failure whose
# cause is invisible. What must not move is the bytecode LEVEL, and that is
# pinned where it can be — the Gradle toolchain targets 21, and a JRE newer
# than that runs it.
FROM cgr.dev/chainguard/jre:latest

# Already unprivileged in the base; stated here and in the chart where a
# reader and an admission policy can both see it.
USER 65532:65532

COPY build/libs/stat.jar /app/stat.jar

ENTRYPOINT ["java", "-jar", "/app/stat.jar"]
