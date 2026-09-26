#!/usr/bin/env bash
#
# Run the chart's OWN post-install/post-upgrade verification hook exactly as
# a platform would: `verification.enabled=true` on the SAME packaged chart
# `example-install` already applied, reusing that release's own values
# (`--reuse-values`) so the only thing this changes is turning the hook on.
# That proves the Job the release actually SHIPS completes against a running
# install — not a copy of the chart, and not the suite run directly by
# `example-smoke`, which never goes through the Job, its ServiceAccount or
# its scoped RBAC at all.
#
# This is what would have caught the suite needing `helm` on its own PATH to
# resolve the kind fixture's names: that only shows up once the suite runs
# AS the hook, inside the cluster, with no fixture and no helm binary in its
# image (see ../e2e/Dockerfile) — a chart-render test or a `just
# example-smoke` run from this box's own devbox environment, where helm IS
# on PATH, never exercises it.
#
# `helm upgrade --wait` reports a hook Job's failure as the command's own
# exit status — hooks are executed and waited on synchronously regardless of
# `--wait`, which here only also covers the release's ordinary resources
# (already ready, since nothing about them changes) — so a failing hook
# fails this script the same way it would fail a platform's own sync.
#
# `hook-delete-policy: before-hook-creation` (templates/verification.yaml)
# means this run's own Job and Pod are still there once `helm upgrade`
# returns, on success OR failure, which is what lets this read the Job's log
# unconditionally afterwards rather than only on a failure it could
# anticipate.
#
# Leaves `verification.enabled` at true on the release. The next
# `example-install` sets every value explicitly again, with no
# `--reuse-values`, so it reverts to the chart's own default (false) on its
# own — nothing here needs to turn it back off.
set -euo pipefail

# The cluster this example is installed into, BY NAME — see hack/install.sh's
# identical comment for why this is never "whatever context happens to be
# current".
KCTX=${KCTX:-kind-policy}
kubectl() { command kubectl --context "$KCTX" "$@"; }
helm() { command helm --kube-context "$KCTX" "$@"; }

NS=${NS:-shortener}
APP=${APP:-example}

# The packaged chart, on the same terms as hack/install.sh: the newest .tgz
# under dist/charts, unless the caller names one.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
CHART_TGZ=${CHART_TGZ:-$(find "$ROOT/dist/charts" -name 'url-shortener-*.tgz' 2>/dev/null | sort -V | tail -1)}
if [ -z "$CHART_TGZ" ]; then
    echo "no packaged chart under dist/charts — run 'just example-snapshot' first" >&2
    exit 1
fi

echo "==> enabling the verification hook on $APP ($CHART_TGZ)"
set +e
helm upgrade "$APP" "$CHART_TGZ" -n "$NS" --reuse-values \
    --set verification.enabled=true \
    --wait --timeout 8m
status=$?
set -e

echo "==> the verify Job's own log"
kubectl -n "$NS" logs "job/${APP}-verify" --all-containers --tail=-1 2>&1 || true

if [ "$status" -ne 0 ]; then
    echo "verification hook FAILED (helm upgrade exited $status) — see the Job's log above" >&2
    exit "$status"
fi

echo "the verification hook completed"
