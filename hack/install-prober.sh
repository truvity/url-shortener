#!/usr/bin/env bash
#
# Enable the url-shortener-e2e chart's PROBER (charts/url-shortener-e2e/
# templates/prober.yaml) on the release hack/install-e2e-chart.sh already
# installed, and prove its journeys succeed within a bounded time — this is
# the THIRD workload the pair of charts ships, distinct from the e2e Job:
# docs/guides/testing.md's "separate from the e2e suite" and
# charts/url-shortener-e2e/values.yaml's own comment on `prober`.
#
# `--reuse-values`, so every name hack/install-e2e-chart.sh already resolved
# (the database, the stream, the archive bucket, the local S3 stand-in's
# static credentials) carries over unchanged — this script's own job is
# only to flip the prober on, never to re-derive names a sibling script
# already got right.
set -euo pipefail

# The cluster this example is installed into, BY NAME — see hack/install.sh's
# identical comment for why this is never "whatever context happens to be
# current".
KCTX=${KCTX:-kind-policy}
kubectl() { command kubectl --context "$KCTX" "$@"; }
helm() { command helm --kube-context "$KCTX" "$@"; }

NS=${NS:-shortener}
APP=${APP:-example}
# The test chart's OWN release, matching hack/install-e2e-chart.sh's default.
RELEASE=${RELEASE:-${APP}-e2e}
INTERVAL=${PROBER_INTERVAL:-5s}

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART_TGZ=${CHART_TGZ:-$(find "$ROOT/dist/charts" -name 'url-shortener-e2e-*.tgz' 2>/dev/null | sort -V | tail -1)}
if [ -z "$CHART_TGZ" ]; then
    echo "no packaged url-shortener-e2e chart under dist/charts — run 'just example-snapshot' first" >&2
    exit 1
fi

echo "==> enabling the prober on $RELEASE (interval=$INTERVAL)"
helm upgrade "$RELEASE" "$CHART_TGZ" -n "$NS" --reuse-values \
    --set prober.enabled=true \
    --set "prober.interval=${INTERVAL}"

DEPLOY="${RELEASE}-prober"
echo "==> waiting for deployment/$DEPLOY"
kubectl -n "$NS" rollout status "deployment/$DEPLOY" --timeout=2m

# The prober reports every pass as a structured log line — see
# examples/url-shortener/e2e/cmd/prober's own `record`. Reading logs rather
# than a metric here is deliberate: this box carries no OTel collector
# (docs/decisions/0005-kind-is-the-gate.md's amendment), so the metrics this
# workload also emits are never exported anywhere this script could read
# them back from — the log line is the one signal that exists on kind
# regardless.
echo "==> waiting up to 60s for a successful journey"
deadline=$((SECONDS + 60))
success=0
while [ "$SECONDS" -lt "$deadline" ]; do
    if kubectl -n "$NS" logs "deployment/$DEPLOY" --tail=500 2>/dev/null \
            | grep -q '"msg":"probe journey succeeded"'; then
        success=1
        break
    fi
    sleep 3
done

if [ "$success" -ne 1 ]; then
    echo "the prober never logged a successful journey within 60s — its own log:" >&2
    kubectl -n "$NS" logs "deployment/$DEPLOY" --tail=500 || true
    exit 1
fi

echo "the prober's journeys are succeeding"
