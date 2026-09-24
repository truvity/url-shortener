# The runtime image for a TypeScript component.
#
# ONE stage, and no RUN line — service.md §7. The server is bundled into a
# single file and the page is built, both OUTSIDE the image by
# `just example-images`; this copies them. There is nothing in here that can
# execute while the image is assembled, so it can be built for another
# architecture without emulation.
#
# No `node_modules`, and that is the bundle's doing rather than a tidiness
# preference: the dependency on this repository's own loader is a SYMLINK in
# a checkout, and a symlink is not a thing that can be copied into a
# container.
#
# The base is by tag rather than by digest, the same choice ../.ko.yaml,
# ../log/Dockerfile and ../stat/Dockerfile make: the registry's free tier
# keeps only the current build, so a pinned digest stops resolving and
# breaks every fork with a failure whose cause is invisible.
FROM cgr.dev/chainguard/node:latest

USER 65532:65532

COPY dist /app

ENTRYPOINT ["node", "/app/server/main.js"]
