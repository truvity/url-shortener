#!/usr/bin/env bash
#
# Stands in for the url-shortener-infra chart on the local cluster: the
# database, the two roles, the stream and the bucket the application
# chart's values point at — created under the EXACT names
# examples/url-shortener/e2e/fixture/names.go reads off the charts, not
# repeated here by hand.
#
# Idempotent and re-runnable on a laptop, on the same terms as hack/kind/up.sh:
# a password already in a Secret is kept, a database or role that already
# exists is left alone, a stream or consumer already there is not recreated.
set -euo pipefail
cd "$(dirname "$0")/../.."

KCTX=${KCTX:-kind-policy}
kubectl() { command kubectl --context "$KCTX" "$@"; }

step() { printf '\n\033[1m==> %s\033[0m\n' "$1"; }

# Every name below comes from THIS, not from a convention repeated by hand —
# see fixture/names.go's doc comment for why. Only APP names the release
# that matters here: the infra chart is never installed, only templated,
# and it is templated under this SAME release name — see that doc comment
# for why there is no separate "infra release" any more.
eval "$(go run ./e2e/fixture/cmd/resolve \
  -namespace "${NS:-shortener}" \
  -app-release "${APP:-example}" \
  -bucket "${BUCKET:-url-shortener-archive}")"

kubectl get namespace "$NAMESPACE" >/dev/null 2>&1 || kubectl create namespace "$NAMESPACE"

step "the owner role's credential"
if ! kubectl -n "$NAMESPACE" get secret "$OWNER_SECRET" >/dev/null 2>&1; then
  kubectl -n "$NAMESPACE" create secret generic "$OWNER_SECRET" \
    --type=kubernetes.io/basic-auth \
    --from-literal=username="$OWNER_ROLE" \
    --from-literal=password="$(head -c 24 /dev/urandom | base64 | tr -d '/+=')"
fi
owner_password=$(kubectl -n "$NAMESPACE" get secret "$OWNER_SECRET" -o jsonpath='{.data.password}' | base64 -d)

step "the runtime role's credential"
if ! kubectl -n "$NAMESPACE" get secret "$APP_SECRET" >/dev/null 2>&1; then
  kubectl -n "$NAMESPACE" create secret generic "$APP_SECRET" \
    --type=kubernetes.io/basic-auth \
    --from-literal=username="$APP_ROLE" \
    --from-literal=password="$(head -c 24 /dev/urandom | base64 | tr -d '/+=')"
fi
app_password=$(kubectl -n "$NAMESPACE" get secret "$APP_SECRET" -o jsonpath='{.data.password}' | base64 -d)

step "the database and its two roles"
# Idempotent by construction rather than by catching an error: Postgres has
# no `CREATE ROLE IF NOT EXISTS`, so existence is asked first. The password
# is (re)applied every run regardless — that converges a role that already
# existed with a credential from an EARLIER run onto the Secret this run is
# actually handing the application, rather than leaving the two to quietly
# disagree.
kubectl -n postgres exec -i deploy/postgres -c postgres -- psql -U postgres -v ON_ERROR_STOP=1 <<SQL
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$OWNER_ROLE') THEN
    CREATE ROLE $OWNER_ROLE LOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = '$APP_ROLE') THEN
    CREATE ROLE $APP_ROLE LOGIN;
  END IF;
END
\$\$;
ALTER ROLE $OWNER_ROLE WITH PASSWORD '$owner_password';
ALTER ROLE $APP_ROLE WITH PASSWORD '$app_password';
SELECT 'CREATE DATABASE $DATABASE OWNER $OWNER_ROLE'
  WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = '$DATABASE') \gexec
SQL

nats_box_image=$(grep '^NATS_BOX_IMAGE=' ../../hack/kind/versions.env | cut -d= -f2-)

step "the stream"
if ! kubectl -n nats run "fixture-stream-$RANDOM" --rm -i --restart=Never --image "$nats_box_image" --command -- sh -c "
    nats --server nats://nats.nats.svc:4222 stream info '$STREAM' >/dev/null 2>&1 && exit 0
    nats --server nats://nats.nats.svc:4222 stream add '$STREAM' \
      --subjects '$REDIRECT_SUBJECT,$REQUEST_SUBJECT' \
      --storage memory --retention limits --discard old \
      --max-msgs=-1 --max-bytes=-1 --max-age=-1 --max-msg-size=-1 --max-consumers=-1 \
      --dupe-window=2m --replicas 1 --no-allow-rollup --no-deny-delete --no-deny-purge --defaults
  " >/tmp/fixture-stream.log 2>&1; then
  cat /tmp/fixture-stream.log >&2
  exit 1
fi
rm -f /tmp/fixture-stream.log

step "the durable consumers stat and log bind to"
for pair in "$STAT_CONSUMER:$REDIRECT_SUBJECT" "$LOG_CONSUMER:$REQUEST_SUBJECT"; do
  durable=${pair%%:*}
  subject=${pair#*:}
  kubectl -n nats run "fixture-consumer-$RANDOM" --rm -i --restart=Never --image "$nats_box_image" --command -- sh -c "
      nats --server nats://nats.nats.svc:4222 consumer info '$STREAM' '$durable' >/dev/null 2>&1 && exit 0
      nats --server nats://nats.nats.svc:4222 consumer add '$STREAM' '$durable' \
        --filter '$subject' --ack explicit --pull --deliver all \
        --max-deliver 5 --wait 30s --defaults
    "
done

step "the archive bucket"
kubectl -n object-store exec deploy/s3 -- sh -c "awslocal s3 mb s3://$BUCKET >/dev/null 2>&1 || true"

echo
echo "the fixture is in place: $DATABASE_HOST/$DATABASE, roles $OWNER_ROLE and $APP_ROLE, stream $STREAM, bucket $BUCKET"
