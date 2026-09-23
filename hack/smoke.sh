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
echo "smoke passed: migrate, redirect, the broker and the counter all did their part"
