#!/usr/bin/env bash
#
# Prove the example WORKS, which is a different question from whether it
# installed.
#
# It writes a row as the database's owner, asks the redirect service for the
# key, and then waits for the counter to move. That last step is the one
# worth having: the counter only moves if the redirect published an event,
# the broker kept it, the consumer was bound to the right subject, and the
# runtime role had rights on a table it did not create. None of those can be
# checked by rendering a chart, and every one of them has broken here.
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

# EXACTLY eight characters: the column holds eight and the route declares
# eight, so any other length fails as a truncation or as a 422 rather than
# as the thing under test.
#
# Built with no pipe. The obvious `tr -dc ... < /dev/urandom | head -c 5`
# ends the script: head exits at five characters, tr is killed by SIGPIPE,
# and `pipefail` reports the 141. Under `set -e` that is a script that dies
# where it meant to generate a name.
KEY="smk$(printf '%05d' $((RANDOM % 100000)))"
LONG_URL="https://example.com/smoke/$KEY"
# The stats row is keyed by the hash of the long URL, so the test can look up
# exactly the row it expects rather than counting rows and hoping.
ID=$(printf '%s' "$LONG_URL" | sha256sum | cut -d' ' -f1)

psql() {
    kubectl -n "$NS" exec "${INFRA}-pg-1" -c postgres -- \
        psql -qtAX -d url_shortener -c "$1"
}

echo "==> a URL to shorten"
psql "INSERT INTO urls.urls (id, url_key, long_url, created_at)
      VALUES ('$ID', '$KEY', '$LONG_URL', now())
      ON CONFLICT (id) DO NOTHING;" >/dev/null

echo "==> asking for it"
# Port-forwarded rather than exec'd into: the service's image carries the
# binary and nothing else, which is the point of it, so there is no shell in
# there to curl from.
kubectl -n "$NS" port-forward "svc/${APP}-redirect" 18080:8080 >/dev/null 2>&1 &
forward=$!
trap 'kill $forward 2>/dev/null || true' EXIT

for _ in $(seq 1 30); do
    curl -fsS -o /dev/null "http://127.0.0.1:18080/version" 2>/dev/null && break
    sleep 1
done

# Both facts from ONE request, because every request is counted and the
# assertion below is an exact number.
read -r status location < <(
    curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' "http://127.0.0.1:18080/r/$KEY"
)

if [ "$status" != "302" ]; then
    echo "SMOKE: the redirect answered $status, wanted 302" >&2
    exit 1
fi

if [ "$location" != "$LONG_URL" ]; then
    echo "SMOKE: redirected to '$location', wanted '$LONG_URL'" >&2
    exit 1
fi

echo "    302 -> $location"

# A second request, so the assertion is that the counter COUNTS rather than
# that a row appeared. An upsert that overwrote instead of incrementing
# would pass a check for "the row exists".
curl -s -o /dev/null "http://127.0.0.1:18080/r/$KEY"

echo "==> waiting for the counter"
# Asynchronous by design: the redirect answers, publishes, and is done. So
# this polls rather than asserting once, and reports what it saw when it
# gives up.
count=0
for _ in $(seq 1 30); do
    count=$(psql "SELECT click_count FROM stats.stats WHERE id = '$ID';" | tr -d '[:space:]')
    [ "${count:-0}" -eq 2 ] 2>/dev/null && break
    sleep 1
done

if [ "${count:-0}" -ne 2 ]; then
    echo "SMOKE: the counter reached '${count:-nothing}' after two redirects, wanted exactly 2" >&2
    echo "       the redirect answered, so the break is between the publisher and the counter:" >&2
    kubectl -n "$NS" logs -l app.kubernetes.io/component=stat --tail=20 >&2
    exit 1
fi

echo "    click_count = $count"

# HOW it moved, not just that it did. The counter holds no database
# credential any more, so the only way that number changed is an RPC to the
# service that owns the table — but a smoke test that stops at the number
# would pass just the same if somebody gave the counter its password back.
echo "==> the counter asked rather than wrote"
# Captured first, then searched. `kubectl ... | grep -q` looks right and is
# not: grep exits at the first match, kubectl is killed by SIGPIPE, and
# `pipefail` reports the 141 — so the check fails hardest exactly when it
# should pass. The same trap already cost this example a working key
# generator; see the note above KEY.
urlslog=$(kubectl -n "$NS" logs -l app.kubernetes.io/component=urls --tail=200 2>/dev/null || true)
if ! printf '%s' "$urlslog" | grep -q '"procedure":"/urlshortener.v1.UrlsService/RecordClick"'; then
    echo "SMOKE: the URL service never served RecordClick, so the count came from somewhere else" >&2
    printf '%s\n' "$urlslog" | tail -30 >&2
    exit 1
fi
echo "    UrlsService/RecordClick was served"

# A Connect unary call is an ordinary POST with a JSON body. That is a claim
# the RPC guide makes about this shape, and it is worth proving rather than
# repeating: it is the difference between a boundary anyone can ask a
# question of and one that needs a generated client.
echo "==> the same boundary answers a plain POST"
kubectl -n "$NS" port-forward "svc/${APP}-urls" 18090:8080 >/dev/null 2>&1 &
urlsforward=$!
trap 'kill $forward $urlsforward 2>/dev/null || true' EXIT

for _ in $(seq 1 30); do
    curl -fsS -o /dev/null -X POST -H 'Content-Type: application/json' -d '{}' \
        "http://127.0.0.1:18090/urlshortener.v1.MetaService/GetVersion" 2>/dev/null && break
    sleep 1
done

version=$(curl -fsS -X POST -H 'Content-Type: application/json' -d '{}' \
    "http://127.0.0.1:18090/urlshortener.v1.MetaService/GetVersion")

if ! printf '%s' "$version" | grep -q '"component":"urls"'; then
    echo "SMOKE: GetVersion answered '$version', which does not name the component" >&2
    exit 1
fi
echo "    $version"

# The front end, which is the fourth language in this example and the second
# consumer of the boundary. It holds no database credential either: the page
# it serves is assembled from an answer the URL service gave it.
echo "==> the page, and what it asked for"
kubectl -n "$NS" port-forward "svc/${APP}-web" 18100:8080 >/dev/null 2>&1 &
webforward=$!
trap 'kill $forward $urlsforward $webforward 2>/dev/null || true' EXIT

for _ in $(seq 1 30); do
    curl -fsS -o /dev/null "http://127.0.0.1:18100/" 2>/dev/null && break
    sleep 1
done

page=$(curl -fsS "http://127.0.0.1:18100/")
if ! printf '%s' "$page" | grep -q 'id="root"'; then
    echo "SMOKE: the front end did not serve its page" >&2
    exit 1
fi

# The part worth asserting: the page's own server asked the URL service and
# got THIS key back. A front end that served a page and reached nothing
# would pass the check above.
answer=$(curl -fsS "http://127.0.0.1:18100/api/url?key=$KEY")
if ! printf '%s' "$answer" | grep -q "$LONG_URL"; then
    echo "SMOKE: the front end answered '$answer', which does not carry the URL it asked about" >&2
    kubectl -n "$NS" logs -l app.kubernetes.io/component=web --tail=20 >&2
    exit 1
fi
echo "    $answer"

echo "==> waiting for the archive"
# The other consumer of the same stream, and the one that proves the rest of
# the contracts hold for a component that is not written in Go: it read the
# configuration this chart rendered, validated it against a schema carried
# inside its own wheel, bound a durable consumer on a DIFFERENT subject, and
# wrote to a store it reached by endpoint rather than by vendor.
#
# The install sets the batch limits low, so this is seconds rather than the
# default minute.
BUCKET="${BUCKET:-url-shortener-archive}"
objects=""
for _ in $(seq 1 60); do
    objects=$(kubectl -n object-store exec deploy/s3 -- \
        awslocal s3 ls "s3://$BUCKET/url-shortener/requests/" --recursive 2>/dev/null || true)
    [ -n "$objects" ] && break
    sleep 2
done

if [ -z "$objects" ]; then
    echo "SMOKE: nothing was archived to s3://$BUCKET after two requests" >&2
    kubectl -n "$NS" logs -l app.kubernetes.io/component=log --tail=30 >&2
    exit 1
fi

# An object is not the same as a record in it. Read the newest one back and
# require the subject the archiver was bound to — an empty object, or one
# holding somebody else's events, would pass a check for "a key exists".
newest=$(printf '%s\n' "$objects" | awk '{print $NF}' | sort | tail -1)
body=$(kubectl -n object-store exec deploy/s3 -- \
    awslocal s3 cp "s3://$BUCKET/$newest" - 2>/dev/null)

if ! printf '%s' "$body" | grep -q '"subject":"url-shortener.log"'; then
    echo "SMOKE: $newest does not hold a request record:" >&2
    printf '%s\n' "$body" | head -5 >&2
    exit 1
fi

echo "    $newest holds $(printf '%s\n' "$body" | grep -c . ) record(s)"
echo "smoke passed: migrate, the boundary, redirect, the page, the broker, the counter and the archive all did their part"
