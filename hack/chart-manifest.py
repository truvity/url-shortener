#!/usr/bin/env python3
"""Turn what the image build just pushed into a helmctl release manifest.

`helmctl package --manifest` takes one file per chart carrying the version,
the appVersion and the values to bake into values.yaml. This writes the
application chart's, pinning every component to the digest the build
reported.

It exists because a digest only exists once the image is built, and the
chart used to be published before that happened -- so the published chart
carried empty image values and could not render a Deployment. Nothing in
the repository could see it: every test supplied images of its own.

Reads `component=digest` lines on stdin, which is what publish-images.sh
prints. A component missing from that input is an error rather than an
entry left blank: blank is what `--require-image-digests` exists to catch,
and catching it here names the component instead.
"""

import argparse
import sys

COMPONENTS = ["migrate", "redirect", "urls", "stat", "log", "web"]


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--version", required=True, help="the release version, without a leading v")
    parser.add_argument("--registry", default="ghcr.io")
    parser.add_argument("--repository", required=True, help="the image path under the registry")
    parser.add_argument("--output", required=True)
    args = parser.parse_args()

    digests = {}
    for line in sys.stdin:
        line = line.strip()
        if not line or "=" not in line:
            continue
        component, digest = line.split("=", 1)
        if component in COMPONENTS:
            digests[component] = digest

    missing = [c for c in COMPONENTS if not digests.get(c)]
    if missing:
        print(f"no digest for: {', '.join(missing)}", file=sys.stderr)
        print("a chart published without one installs nothing, and says so only in a cluster", file=sys.stderr)
        return 1

    lines = [
        f"version: {args.version}",
        f"appVersion: {args.version}",
        "values:",
        "  images:",
    ]
    for component in COMPONENTS:
        lines += [
            f"    {component}:",
            f"      registry: {args.registry}",
            f"      repository: {args.repository}/{component}",
            f'      tag: "{args.version}"',
            f"      digest: {digests[component]}",
        ]

    with open(args.output, "w", encoding="utf-8") as handle:
        handle.write("\n".join(lines) + "\n")

    print(f"wrote {args.output}: {len(COMPONENTS)} components, digest-pinned", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
