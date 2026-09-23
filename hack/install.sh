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

# The archive's bucket and the credential to reach it. Both belong to the
# PLATFORM, not to either chart: a service does not create its own store, for
# the same reason it does not create its own database — a component that can
# create a bucket can create it in the wrong account, with the wrong
# retention, and nothing notices until somebody looks.
BUCKET="${BUCKET:-url-shortener-archive}"
BUCKET_SECRET="${INFRA}-archive"

kubectl get namespace "$NS" >/dev/null 2>&1 || kubectl create namespace "$NS"

echo "==> the archive's bucket"
kubectl -n object-store exec deploy/s3 -- awslocal s3 mb "s3://$BUCKET" >/dev/null 2>&1 || true

if ! kubectl -n "$NS" get secret "$BUCKET_SECRET" >/dev/null 2>&1; then
    # The local store accepts anything; a real one would not, and the shape
    # is the same either way — the chart takes the NAME of a secret, the
    # configuration file takes the NAMES of two variables, and no value
    # appears in anything that is rendered or committed.
    kubectl -n "$NS" create secret generic "$BUCKET_SECRET" \
        --from-literal=accessKeyID=test \
        --from-literal=secretAccessKey=test
fi

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
    --set "archive.bucket.name=$BUCKET" \
    --set archive.bucket.endpoint=http://s3.object-store.svc:4566 \
    --set archive.bucket.region=us-east-1 \
    --set archive.bucket.pathStyle=true \
    --set "archive.bucket.credentialsSecret=$BUCKET_SECRET" \
    --set archive.batch.maxRecords=5 \
    --set archive.batch.maxSeconds=5 \
    --wait --timeout 8m

kubectl -n "$NS" get pods
