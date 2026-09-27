#!/usr/bin/env bash
#
# Install the url-shortener-e2e TEST CHART — the PACKAGED .tgz
# `just example-snapshot` produced, never the source directory, on the same
# terms as hack/install.sh: docs/contracts/release.md §7, a published
# artifact is tested as published.
#
# This is the SECOND way the same suite runs (docs/guides/testing.md): a
# Job the chart renders, applied after the application release
# (hack/install.sh) is already up in the SAME namespace, reading every name
# the suite needs from the chart's own values rather than from this box's
# fixture directly — see charts/url-shortener-e2e/values.yaml. It proves
# what `just example-smoke` cannot: that the suite still runs every case
# when it is the Job the chart ships, through that Job's own scoped RBAC,
# with no fixture and no `helm` in its image (examples/url-shortener/e2e/Dockerfile)
# — the same gap hack/verify-hook.sh exists to close for the application
# chart's OWN hook, closed here for this chart's plain Job instead.
set -euo pipefail

# The cluster this example is installed into, BY NAME — see hack/install.sh's
# identical comment for why this is never "whatever context happens to be
# current".
KCTX=${KCTX:-kind-policy}
kubectl() { command kubectl --context "$KCTX" "$@"; }
helm() { command helm --kube-context "$KCTX" "$@"; }

NS=${NS:-shortener}
APP=${APP:-example}
BUCKET=${BUCKET:-url-shortener-archive}
# full | tenant — see charts/url-shortener-e2e/values.yaml's own comment on
# `mode` for what each one runs.
MODE=${MODE:-full}
# The test chart's OWN release, separate from $APP: this chart tests
# somebody else's release rather than sharing its name.
RELEASE=${RELEASE:-${APP}-e2e}

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART_TGZ=${CHART_TGZ:-$(find "$ROOT/dist/charts" -name 'url-shortener-e2e-*.tgz' 2>/dev/null | sort -V | tail -1)}
if [ -z "$CHART_TGZ" ]; then
    echo "no packaged url-shortener-e2e chart under dist/charts — run 'just example-snapshot' first" >&2
    exit 1
fi

# The names hack/install.sh installed the application UNDER — read the
# SAME way that script reads them, from the charts rather than repeated
# here by hand. See examples/url-shortener/e2e/fixture/names.go's own doc
# comment for why a shared resolver, and not a copy of the convention, is
# what keeps every caller agreeing.
eval "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && go run ./e2e/fixture/cmd/resolve \
  -namespace "$NS" -app-release "$APP" -bucket "$BUCKET")"

# The local S3 stand-in, on the same terms as hack/install.sh's own
# LOCAL_STORE_ARGS: unset means the SDK's ambient credentials, which is
# right off this box and wrong on it — the kind lane has no identity plane
# to hand the suite instead (docs/decisions/0005-kind-is-the-gate.md's
# amendment). archive.credentialsSecret carries STATIC credentials for
# that endpoint — an ambient-credential lookup (IMDS) times out on this
# box, same reason the application itself is handed a Secret rather than
# left to the SDK's own default chain — reusing the EXACT Secret
# hack/install.sh already created for the application
# ($APP-archive, static test/test credentials), so nothing here mints a
# second copy of it.
LOCAL_STORE_ARGS=""
if kubectl get namespace object-store >/dev/null 2>&1; then
    LOCAL_STORE_ARGS="--set archive.endpoint=http://s3.object-store.svc:4566 --set archive.region=us-east-1"
    LOCAL_STORE_ARGS="$LOCAL_STORE_ARGS --set archive.credentialsSecret=${APP}-archive"
fi

# This chart's Job is named for its OWN version (see
# charts/url-shortener-e2e/templates/_helpers.tpl's
# "url-shortener-e2e.jobName"), which on this box never moves off
# Chart.yaml's 0.0.0 placeholder — so a second run of this script for the
# SAME snapshot names the SAME immutable Job `helm upgrade --install`
# cannot change in place. The chart no longer sets
# ttlSecondsAfterFinished by default (charts/url-shortener-e2e/values.yaml
# — a GitOps controller's self-heal would recreate a Job that deleted
# itself), so nothing here can lean on a TTL to have cleared the previous
# run's Job either. Delete it explicitly, before helm ever tries.
kubectl -n "$NS" delete job \
    -l "app.kubernetes.io/instance=${RELEASE},app.kubernetes.io/component=e2e" \
    --ignore-not-found

echo "==> the test chart ($CHART_TGZ), mode=$MODE"
# shellcheck disable=SC2086 # LOCAL_STORE_ARGS is deliberately word-split —
# see hack/install.sh's identical comment.
helm upgrade --install "$RELEASE" "$CHART_TGZ" -n "$NS" \
    $LOCAL_STORE_ARGS \
    --set appRelease="$APP" \
    --set mode="$MODE" \
    --set "database.host=${DATABASE_HOST}" \
    --set "database.name=${DATABASE}" \
    --set "database.owner.role=${OWNER_ROLE}" \
    --set "database.app.role=${APP_ROLE}" \
    --set "database.app.passwordSecret=${APP_SECRET}" \
    --set "events.stream=${STREAM}" \
    --set "events.redirectSubject=${REDIRECT_SUBJECT}" \
    --set "events.requestSubject=${REQUEST_SUBJECT}" \
    --set "events.statConsumer=${STAT_CONSUMER}" \
    --set "events.logConsumer=${LOG_CONSUMER}" \
    --set "archive.bucket=${BUCKET}"

# The Job's own name folds in this chart's version (see
# charts/url-shortener-e2e/templates/_helpers.tpl's "url-shortener-e2e.jobName"),
# so it is found by label rather than reconstructed here — the label
# selector is the one thing this script and that template cannot drift on.
# Newest first (kubectl's jsonpath has no portable negative index), in case
# an OLDER version's Job is still around waiting on its own
# ttlSecondsAfterFinished.
JOB=$(kubectl -n "$NS" get job \
    -l "app.kubernetes.io/instance=${RELEASE},app.kubernetes.io/component=e2e" \
    --sort-by=.metadata.creationTimestamp \
    -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | tail -1)
if [ -z "$JOB" ]; then
    echo "no e2e Job found for release $RELEASE in $NS" >&2
    exit 1
fi

echo "==> waiting for job/$JOB"
set +e
kubectl -n "$NS" wait "job/$JOB" --for=condition=complete --timeout=8m
status=$?
if [ "$status" -ne 0 ]; then
    # A Job that reached Failed (backoffLimit exhausted) answers `wait
    # --for=condition=complete` with a non-zero exit and no error of its
    # own worth reading — the Job's own log is the one that says why.
    kubectl -n "$NS" get "job/$JOB" -o jsonpath='{.status}'
    echo
fi
set -e

echo "==> job/$JOB's own log"
kubectl -n "$NS" logs "job/$JOB" --all-containers --tail=-1 2>&1 || true

if [ "$status" -ne 0 ]; then
    echo "the e2e test chart's Job FAILED — see the log above" >&2
    exit "$status"
fi

echo "the e2e test chart's Job completed"
