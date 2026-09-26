#!/usr/bin/env bash
#
# Install the example's APPLICATION chart into the local cluster — the
# PACKAGED .tgz `just example-snapshot` produced from a real image build,
# never the source directory. docs/contracts/release.md §7: a published
# artifact is tested as published, and the chart with its digests baked in
# by helmctl is the only version of it a consumer ever installs.
#
# It installs ONLY this chart. On a public repository's kind box there is no
# infra release: the database, the two roles, the stream and the bucket
# every value below points at are provided by
# examples/url-shortener/e2e/fixture/apply.sh, which must have already run
# — `just example-fixture`, before `just example-install`; `just cluster-all`
# runs them in that order. See docs/decisions/0005-kind-is-the-gate.md for
# why kind installs no infra chart at all.
set -euo pipefail

# The cluster this example is installed into, BY NAME.
#
# Not "whatever context happens to be current". `kind create cluster` points
# the current context at whatever it just made, so a second box created in
# another terminal silently moves every `kubectl` in this script — and the
# symptom is "namespaces not found" for a namespace that is right there, in
# the cluster you thought you were talking to. It also means this script
# cannot be aimed at a real cluster by accident.
KCTX=${KCTX:-kind-policy}
kubectl() { command kubectl --context "$KCTX" "$@"; }
helm() { command helm --kube-context "$KCTX" "$@"; }

NS=${NS:-shortener}
APP=${APP:-example}
BUCKET=${BUCKET:-url-shortener-archive}

# The packaged chart `just example-snapshot` produced — helmctl, from a real
# push to the box's registry, the same tool and the same file a release
# packages from. NOT `--set images.*`: the chart's own values already carry
# every digest, and setting them here would be a second, competing source
# for the one thing helmctl refuses to publish empty.
#
# The newest .tgz under dist/charts, unless the caller names one — a real
# cluster's installer names a version somebody released instead.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART_TGZ=${CHART_TGZ:-$(find "$ROOT/dist/charts" -name 'url-shortener-*.tgz' 2>/dev/null | sort -V | tail -1)}
if [ -z "$CHART_TGZ" ]; then
    echo "no packaged chart under dist/charts — run 'just example-snapshot' first" >&2
    exit 1
fi

# Anything else the caller wants to set, as helm arguments. A real cluster
# needs values a local one does not — a storage class, a bucket somebody
# provisioned, an account annotation — and they belong to whoever is
# installing rather than to this script.
EXTRA=${EXTRA:-}

# The names the fixture provisioned under, read the SAME way the fixture
# itself read them — from the charts, not repeated here. `apply.sh` and
# this script must agree on every one of them, and a shared resolver is
# what makes agreeing not something either has to remember.
#
# Notably absent below: the stream, its subjects and the two durable
# consumer names. This chart computes all of them from its OWN release
# name by default, and so does the fixture's template of the infra chart
# (fixture/names.go renders it under this SAME release name, on purpose) —
# so as long as both sides agree on APP, neither has to tell the other
# what it decided. Forcing them with `--set` here would only be another
# place for the two to drift.
eval "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && go run ./e2e/fixture/cmd/resolve \
  -namespace "$NS" -app-release "$APP" -bucket "$BUCKET")"

kubectl get namespace "$NS" >/dev/null 2>&1 || kubectl create namespace "$NS"

# Static credentials for the LOCAL store only. A real cluster gives its
# workloads an identity instead, and the configuration's `credentialsEnv`
# stays unset — which is the better answer and the one the bucket fragment
# documents, because an ambient credential leaves nothing to leak.
BUCKET_SECRET="${APP}-archive"
if kubectl get namespace object-store >/dev/null 2>&1 &&
    ! kubectl -n "$NS" get secret "$BUCKET_SECRET" >/dev/null 2>&1; then
    kubectl -n "$NS" create secret generic "$BUCKET_SECRET" \
        --from-literal=accessKeyID=test \
        --from-literal=secretAccessKey=test
fi

LOCAL_STORE_ARGS=""
if kubectl get namespace object-store >/dev/null 2>&1; then
    LOCAL_STORE_ARGS="--set archive.bucket.endpoint=http://s3.object-store.svc:4566"
    LOCAL_STORE_ARGS="$LOCAL_STORE_ARGS --set archive.bucket.region=us-east-1"
    LOCAL_STORE_ARGS="$LOCAL_STORE_ARGS --set archive.bucket.pathStyle=true"
    LOCAL_STORE_ARGS="$LOCAL_STORE_ARGS --set archive.bucket.credentialsSecret=$BUCKET_SECRET"
fi

echo "==> the application ($CHART_TGZ)"
# shellcheck disable=SC2086 # EXTRA and the two value strings above are
# deliberately word-split: each is a list of helm arguments, and quoting it
# would pass the whole list as one.
helm upgrade --install "$APP" "$CHART_TGZ" -n "$NS" \
    $LOCAL_STORE_ARGS $EXTRA \
    --set "database.host=${DATABASE_HOST}" \
    --set "database.owner.passwordSecret=${OWNER_SECRET}" \
    --set "database.app.passwordSecret=${APP_SECRET}" \
    --set events.url=nats://nats.nats.svc:4222 \
    --set "archive.bucket.name=$BUCKET" \
    --set archive.batch.maxRecords=5 \
    --set archive.batch.maxSeconds=5 \
    --wait --timeout 8m

kubectl -n "$NS" get pods
