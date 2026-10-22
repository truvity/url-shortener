#!/usr/bin/env bash
# The selection rule of prune-ci-images.sh, on a fixture: newest N ci-only
# versions are kept, everything else ci-only goes, and a version with any
# other tag or no tag is never selected.
set -euo pipefail
cd "$(dirname "$0")"

fixture='[[
 {"id":1,"created_at":"2026-10-01T00:00:00Z","metadata":{"container":{"tags":["ci-aaaaaaaaaaaa"]}}},
 {"id":2,"created_at":"2026-10-02T00:00:00Z","metadata":{"container":{"tags":["ci-bbbbbbbbbbbb"]}}},
 {"id":3,"created_at":"2026-10-03T00:00:00Z","metadata":{"container":{"tags":["ci-cccccccccccc"]}}},
 {"id":4,"created_at":"2026-10-04T00:00:00Z","metadata":{"container":{"tags":["1.55.0","ci-dddddddddddd"]}}},
 {"id":5,"created_at":"2026-09-01T00:00:00Z","metadata":{"container":{"tags":["1.54.0"]}}},
 {"id":6,"created_at":"2026-09-02T00:00:00Z","metadata":{"container":{"tags":[]}}},
 {"id":7,"created_at":"2026-09-03T00:00:00Z","metadata":{"container":{}}},
 {"id":8,"created_at":"2026-10-05T00:00:00Z","metadata":{"container":{"tags":["ci-eeeeeeeeeeee"]}}}
],[
 {"id":9,"created_at":"2026-10-06T00:00:00Z","metadata":{"container":{"tags":["ci-ffffffffffff"]}}},
 {"id":10,"created_at":"2026-10-07T00:00:00Z","metadata":{"container":{"tags":["latest"]}}}
]]'

got=$(./prune-ci-images.sh --select 2 <<<"$fixture" | sort -n | paste -sd, -)
want="1,2,3"
[ "$got" = "$want" ] || { echo "keep 2: got $got, want $want" >&2; exit 1; }

got=$(./prune-ci-images.sh --select 100 <<<"$fixture" | paste -sd, -)
[ -z "$got" ] || { echo "keep 100: got $got, want nothing" >&2; exit 1; }

got=$(./prune-ci-images.sh --select 0 <<<"$fixture" | sort -n | paste -sd, -)
want="1,2,3,8,9"
[ "$got" = "$want" ] || { echo "keep 0: got $got, want $want" >&2; exit 1; }

echo "prune-ci-images: selection rule ok"
