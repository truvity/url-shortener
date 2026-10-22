#!/usr/bin/env bash
#
# Delete the integration run's image versions from ghcr.io, keeping the newest
# N per image. The integration run pushes every image as ci-<sha> into the same
# packages a release publishes to; nothing else removes them.
#
# What may be deleted: a version whose tags are ALL ci-<something>. A version
# with any other tag (a release's 1.2.3, latest) is never touched, and neither
# is an untagged version: a multi-platform image is an index plus untagged
# per-platform manifests that the index (kept or not) points at, and deleting
# one of those breaks the image it belongs to.
#
#   hack/prune-ci-images.sh                run (needs GH_TOKEN with packages: write)
#   hack/prune-ci-images.sh --dry-run      print what it would delete
#   hack/prune-ci-images.sh --select N     read a versions list (JSON) on stdin and
#                                          print the ids to delete, keeping N newest
#
# KEEP (default 30) and ORG (default the repository owner) are environment.
set -euo pipefail

components=(migrate redirect urls prober stat log web)

# Reads the API's versions list (an array, or several arrays slurped into one)
# on stdin; prints the ids of the ci-only versions beyond the newest $keep.
select_ids() {
    jq -r --argjson keep "$1" '
        flatten
        | map(select((.metadata.container.tags // []) | length > 0
                     and all(.[]; test("^ci-[0-9a-zA-Z._-]+$"))))
        | sort_by(.created_at) | reverse
        | .[$keep:]
        | .[].id'
}

if [ "${1:-}" = "--select" ]; then
    select_ids "${2:?--select needs how many to keep}"
    exit 0
fi

dry=false
[ "${1:-}" = "--dry-run" ] && dry=true

keep="${KEEP:-30}"
org="${ORG:-${GITHUB_REPOSITORY_OWNER:?ORG or GITHUB_REPOSITORY_OWNER is not set}}"
: "${GH_TOKEN:?GH_TOKEN is not set}"

for c in "${components[@]}"; do
    package="url-shortener%2F$c"

    if ! versions=$(gh api --paginate --slurp "/orgs/$org/packages/container/$package/versions?per_page=100" 2>/dev/null); then
        echo "$c: no such package yet (or not readable): skipped"
        continue
    fi

    ids=$(select_ids "$keep" <<<"$versions")
    n=$(grep -c . <<<"$ids" || true)
    echo "$c: $n ci version(s) beyond the newest $keep"

    for id in $ids; do
        if $dry; then
            echo "    would delete $c version $id"
        else
            gh api -X DELETE "/orgs/$org/packages/container/$package/versions/$id"
            echo "    deleted $c version $id"
        fi
    done
done
