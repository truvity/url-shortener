#!/usr/bin/env bash
#
# Prove what a change to the infrastructure chart does to the objects an
# install already has: render the chart as it was at a git ref and as it is in
# this tree, each with its own values, and compare them object by object
# (hack/render-diff). An empty diff means a sync changes nothing; a listed
# difference has to be explained before the change is released.
#
#   prove-infra-render.sh OLD_REF RELEASE NAMESPACE OLD_VALUES NEW_VALUES [ALLOW_RE]
#
# The values are what the install is really given; they are not committed
# (they carry a platform's particulars), so this takes paths. The new values
# differ from the old by what the change asks a platform to add.
set -euo pipefail

if [ "$#" -lt 5 ]; then
    sed -n '3,15p' "$0" >&2
    exit 2
fi
old_ref=$1 release=$2 ns=$3 old_values=$4 new_values=$5 allow=${6:-}

repo="$(cd "$(dirname "$0")/../../.." && pwd)"
chart=examples/url-shortener/charts/url-shortener-infra
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The old chart exactly as it was, with its dependencies as its own lock says.
mkdir "$work/old"
git -C "$repo" archive "$old_ref" "$chart" | tar -x -C "$work/old"
if grep -q '^dependencies:' "$work/old/$chart/Chart.yaml"; then
    helm dependency build --skip-refresh "$work/old/$chart" >/dev/null
fi

# The new chart from this tree, its archive replaced by what the lock says.
rm -rf "$work/new"
mkdir "$work/new"
cp -r "$repo/$chart" "$work/new/chart"
rm -rf "$work/new/chart/charts"
helm dependency build --skip-refresh "$work/new/chart" >/dev/null

helm template "$release" "$work/old/$chart" --namespace "$ns" -f "$old_values" >"$work/old.yaml"
helm template "$release" "$work/new/chart" --namespace "$ns" -f "$new_values" >"$work/new.yaml"

cd "$repo"
go run ./hack/render-diff -namespace "$ns" ${allow:+-allow "$allow"} "$work/old.yaml" "$work/new.yaml"
