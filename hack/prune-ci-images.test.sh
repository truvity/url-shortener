#!/usr/bin/env bash
# The selection rule of prune-ci-images.sh, on a fixture: the newest N ci-only
# versions are kept, the rest go together with the attestations (.sbom, .sig,
# .att) of exactly those versions, and a version with any other tag, no tag,
# or an attestation of something that stays is never selected.
set -euo pipefail
cd "$(dirname "$0")"

h() { printf '%064d' "$1"; }

fixture="[[
 {\"id\":1,\"name\":\"sha256:$(h 1)\",\"created_at\":\"2026-10-01T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"ci-aaaaaaaaaaaa\"]}}},
 {\"id\":2,\"name\":\"sha256:$(h 2)\",\"created_at\":\"2026-10-02T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"ci-bbbbbbbbbbbb\"]}}},
 {\"id\":3,\"name\":\"sha256:$(h 3)\",\"created_at\":\"2026-10-03T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"ci-cccccccccccc\"]}}},
 {\"id\":4,\"name\":\"sha256:$(h 4)\",\"created_at\":\"2026-10-04T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"1.55.0\",\"ci-dddddddddddd\"]}}},
 {\"id\":5,\"name\":\"sha256:$(h 5)\",\"created_at\":\"2026-09-01T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"1.54.0\"]}}},
 {\"id\":6,\"name\":\"sha256:$(h 6)\",\"created_at\":\"2026-09-02T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[]}}},
 {\"id\":7,\"name\":\"sha256:$(h 7)\",\"created_at\":\"2026-09-03T00:00:00Z\",\"metadata\":{\"container\":{}}},
 {\"id\":8,\"name\":\"sha256:$(h 8)\",\"created_at\":\"2026-10-05T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"ci-eeeeeeeeeeee\"]}}},
 {\"id\":20,\"name\":\"sha256:$(h 20)\",\"created_at\":\"2026-10-01T00:00:01Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 1).sbom\"]}}},
 {\"id\":21,\"name\":\"sha256:$(h 21)\",\"created_at\":\"2026-10-01T00:00:02Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 1).sig\"]}}},
 {\"id\":22,\"name\":\"sha256:$(h 22)\",\"created_at\":\"2026-10-02T00:00:01Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 2).att\"]}}},
 {\"id\":23,\"name\":\"sha256:$(h 23)\",\"created_at\":\"2026-10-03T00:00:01Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 3).sbom\"]}}},
 {\"id\":24,\"name\":\"sha256:$(h 24)\",\"created_at\":\"2026-10-04T00:00:01Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 4).sbom\"]}}},
 {\"id\":25,\"name\":\"sha256:$(h 25)\",\"created_at\":\"2026-09-01T00:00:01Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 5).sbom\"]}}},
 {\"id\":26,\"name\":\"sha256:$(h 26)\",\"created_at\":\"2026-10-01T00:00:03Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 1).sbom\",\"1.55.0\"]}}},
 {\"id\":27,\"name\":\"sha256:$(h 27)\",\"created_at\":\"2026-10-01T00:00:04Z\",\"metadata\":{\"container\":{\"tags\":[\"sha256-$(h 99).sbom\"]}}}
],[
 {\"id\":9,\"name\":\"sha256:$(h 9)\",\"created_at\":\"2026-10-06T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"ci-ffffffffffff\"]}}},
 {\"id\":10,\"name\":\"sha256:$(h 10)\",\"created_at\":\"2026-10-07T00:00:00Z\",\"metadata\":{\"container\":{\"tags\":[\"latest\"]}}}
]]"

check() { # keep, want
    got=$(./prune-ci-images.sh --select "$1" <<<"$fixture" | sort -n | paste -sd, -)
    [ "$got" = "$2" ] || { echo "keep $1: got '$got', want '$2'" >&2; exit 1; }
}

# keep 2: versions 1, 2 and 3 are the three oldest ci-only versions beyond the
# newest two (9, 8). Their attestations (20, 21 of 1; 22 of 2; 23 of 3) go.
# Not selected: 24 (attests 4, which has a semver tag so stays), 25 (attests a
# release), 26 (also tagged 1.55.0), 27 (attests something unknown).
check 2 "1,2,3,20,21,22,23"

# keep 100: nothing is beyond the newest 100, so no attestation goes either.
check 100 ""

# keep 0: every ci-only version goes (1, 2, 3, 8, 9) with the attestations of
# those only.
check 0 "1,2,3,8,9,20,21,22,23"

echo "prune-ci-images: selection rule ok"
