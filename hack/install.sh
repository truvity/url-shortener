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
INFRA=${INFRA:-infra}
APP=${APP:-example}

# Where the images come from, and how they are named.
#
# The local box loads them into the node and never pulls, so `kind.local` and
# `latest` are right there and wrong anywhere else. A real cluster pulls from
# a registry, by a version somebody released. Both are values because the
# difference between a local run and a real one should be arguments rather
# than a second script.
IMAGE_REPOSITORY=${IMAGE_REPOSITORY:-kind.local}
IMAGE_TAG=${IMAGE_TAG:-latest}
IMAGE_PULL_POLICY=${IMAGE_PULL_POLICY:-Never}

# Anything else the caller wants to set, as helm arguments. A real cluster
# needs values a local one does not — a storage class, a bucket somebody
# provisioned, an account annotation — and they belong to whoever is
# installing rather than to this script.
EXTRA=${EXTRA:-}

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

# Where the local store is, when there is one. On a real cluster the bucket
# is reached by the SDK's own resolution and the workload's own identity, so
# none of this is set — which is the bucket fragment's documented default.
LOCAL_STORE_ARGS=""
if kubectl get namespace object-store >/dev/null 2>&1; then
    LOCAL_STORE_ARGS="--set archive.bucket.endpoint=http://s3.object-store.svc:4566"
    LOCAL_STORE_ARGS="$LOCAL_STORE_ARGS --set archive.bucket.region=us-east-1"
    LOCAL_STORE_ARGS="$LOCAL_STORE_ARGS --set archive.bucket.pathStyle=true"
    LOCAL_STORE_ARGS="$LOCAL_STORE_ARGS --set archive.bucket.credentialsSecret=$BUCKET_SECRET"
fi

kubectl get namespace "$NS" >/dev/null 2>&1 || kubectl create namespace "$NS"

# The local box runs its own object store and this creates the bucket in it.
# A real cluster's bucket was provisioned by whoever owns the account, which
# is the store rule the platform contract states: a service does not create
# its own store, and neither does its installer.
if kubectl get namespace object-store >/dev/null 2>&1; then
    echo "==> the archive's bucket, in the local store"
    kubectl -n object-store exec deploy/s3 -- awslocal s3 mb "s3://$BUCKET" >/dev/null 2>&1 || true
else
    echo "==> no local object store: expecting $BUCKET to exist already"
fi

# Static credentials for the LOCAL store only. A real cluster gives its
# workloads an identity instead, and the configuration's `credentialsEnv`
# stays unset — which is the better answer and the one the bucket fragment
# documents, because an ambient credential leaves nothing to leak.
if kubectl get namespace object-store >/dev/null 2>&1 &&
    ! kubectl -n "$NS" get secret "$BUCKET_SECRET" >/dev/null 2>&1; then
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
# shellcheck disable=SC2086 # EXTRA is deliberately word-split: it is a list
# of helm arguments, and quoting it would pass them as one.
helm upgrade --install "$APP" "$CHARTS/url-shortener" -n "$NS" \
    --set image.repository="$IMAGE_REPOSITORY" \
    --set image.tag="$IMAGE_TAG" \
    --set image.pullPolicy="$IMAGE_PULL_POLICY" \
    $LOCAL_STORE_ARGS $EXTRA \
    --set "database.host=${INFRA}-pg-rw" \
    --set "database.owner.passwordSecret=${INFRA}-pg-app" \
    --set "database.app.passwordSecret=$RUNTIME_SECRET" \
    --set events.url=nats://nats.nats.svc:4222 \
    --set "archive.bucket.name=$BUCKET" \
    --set archive.batch.maxRecords=5 \
    --set archive.batch.maxSeconds=5 \
    --wait --timeout 8m

kubectl -n "$NS" get pods
