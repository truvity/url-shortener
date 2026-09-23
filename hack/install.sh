#!/usr/bin/env bash
#
# Install the example into the local cluster, the way a deployment installs
# it: two releases, in order.
#
# The order is not a convenience. The application's migration is a
# pre-install hook, and Helm runs every hook before it applies anything else
# in the same release — so the database cannot belong to the release that
# migrates it. The first release creates the database and the stream, and
# the second one waits for them to become real before it installs anything.
set -euo pipefail

NS=${NS:-shortener}
INFRA=${INFRA:-infra}
APP=${APP:-example}
CHARTS="$(cd "$(dirname "${BASH_SOURCE[0]}")/../charts" && pwd)"

# The secret holding the runtime role's password. Neither chart generates it:
# a chart that invented a password would store it in the release's own
# manifest, where anyone who can read a release can read the password.
RUNTIME_SECRET="${INFRA}-pg-runtime"

kubectl get namespace "$NS" >/dev/null 2>&1 || kubectl create namespace "$NS"

if ! kubectl -n "$NS" get secret "$RUNTIME_SECRET" >/dev/null 2>&1; then
    echo "==> the runtime role's credential"
    kubectl -n "$NS" create secret generic "$RUNTIME_SECRET" \
        --type=kubernetes.io/basic-auth \
        --from-literal=username=url_shortener_app \
        --from-literal=password="$(head -c 24 /dev/urandom | base64 | tr -d '/+=')"
fi

echo "==> the database and the stream"
helm upgrade --install "$INFRA" "$CHARTS/url-shortener-infra" -n "$NS" \
    --set "postgres.runtimePasswordSecret=$RUNTIME_SECRET" \
    --wait --timeout 5m

# Installed is not the same as real: a custom resource nothing reconciles is
# accepted and stored and never becomes anything.
echo "==> waiting for the operators to act"
kubectl -n "$NS" wait "cluster/${INFRA}-pg" --for=condition=Ready --timeout=6m
kubectl -n "$NS" wait "stream/${INFRA}-events" --for=condition=Ready --timeout=3m

echo "==> the application"
helm upgrade --install "$APP" "$CHARTS/url-shortener" -n "$NS" \
    --set image.repository="${IMAGE_REPOSITORY:-kind.local}" \
    --set image.tag="${IMAGE_TAG:-latest}" \
    --set image.pullPolicy="${IMAGE_PULL_POLICY:-Never}" \
    --set "database.host=${INFRA}-pg-rw" \
    --set "database.owner.passwordSecret=${INFRA}-pg-app" \
    --set "database.app.passwordSecret=$RUNTIME_SECRET" \
    --set events.url=nats://nats.nats.svc:4222 \
    --wait --timeout 8m

kubectl -n "$NS" get pods
