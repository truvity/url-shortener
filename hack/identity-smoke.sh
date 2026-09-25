#!/usr/bin/env bash
#
# Prove the transport rule ON THE CLUSTER, which is the only place it can be
# proved: the package's tests show what the code does with a certificate, and
# this shows that the platform hands it the right one and refuses the wrong
# caller.
#
# Two probes, and the second is the one worth having. Issuing identities
# correctly while admitting anyone who asks is the failure that looks like
# success from every other angle.
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
TRUST_DOMAIN=${TRUST_DOMAIN:-policy.local}

# The subject the counter's consumer is bound to is computed from the
# namespace and the pair's installName (hack/install.sh and
# charts/url-shortener/templates/_helpers.tpl), not a fixed string. This
# matches install.sh's own default.
INSTALL_NAME=${INSTALL_NAME:-$APP}
CHARTS="$(cd "$(dirname "${BASH_SOURCE[0]}")/../charts" && pwd)"

# `permissive`, not `strict`: the cleartext port is what the rest of the
# smoke test and any port-forward use, and taking it away here would prove
# the transport by breaking everything else.
echo "==> turning the transport on"
helm upgrade "$APP" "$CHARTS/url-shortener" -n "$NS" --reuse-values \
    --set tls.mode=permissive \
    --set "tls.trustDomain=$TRUST_DOMAIN" \
    --set "tls.peers.redirect[0].namespace=$NS" \
    --set "tls.peers.redirect[0].serviceAccount=admitted" \
    --wait --timeout 5m >/dev/null

# `--insecure` on the probe, and it is not a shortcut. What is under test is
# the SERVER'S decision: does it admit this account or close on it. The
# probe's own view of the server is beside the point, and the platform's
# certificates carry no host name to check anyway — they carry an identity,
# which is what the service checks and what the Go tests cover from the
# client's side.
#
# The probes carry a security context for the same reason the chart does: a
# CSI driver writes what it mounts owned by root, so a process running as
# anyone else cannot read its own certificate. The symptom is a complaint
# about the certificate's format, which is not what is wrong with it.
#
# Two callers. Both hold a real identity from the platform; only one is on
# the service's list. That is the distinction under test — not "has a
# certificate" but "is this account allowed".
for who in admitted stranger; do
    kubectl -n "$NS" get serviceaccount "$who" >/dev/null 2>&1 ||
        kubectl -n "$NS" create serviceaccount "$who" >/dev/null
done

kubectl -n "$NS" apply -f - >/dev/null <<YAML
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: probes-request-identity
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: ${APP}-request-identity
subjects:
  - kind: ServiceAccount
    name: admitted
  - kind: ServiceAccount
    name: stranger
YAML

probe() {
    local who=$1 expect=$2 pod="probe-$1"

    kubectl -n "$NS" delete pod "$pod" --ignore-not-found >/dev/null 2>&1
    kubectl -n "$NS" run "$pod" --image=curlimages/curl:8.11.1 --restart=Never --quiet \
        --overrides="$(cat <<JSON
{"spec":{"serviceAccountName":"$who",
 "securityContext":{"runAsNonRoot":true,"runAsUser":100,"runAsGroup":100,"fsGroup":100},
 "containers":[{"name":"probe","image":"curlimages/curl:8.11.1",
   "command":["curl","-sS","-o","/dev/null","-w","%{http_code}","--insecure",
     "--cert","/id/tls.crt","--key","/id/tls.key",
     "https://${APP}-redirect:8443/version"],
   "volumeMounts":[{"name":"id","mountPath":"/id","readOnly":true}]}],
 "volumes":[{"name":"id","csi":{"driver":"spiffe.csi.cert-manager.io","readOnly":true}}]}}
JSON
)" >/dev/null

    local phase=""
    for _ in $(seq 1 45); do
        phase=$(kubectl -n "$NS" get pod "$pod" -o jsonpath='{.status.phase}' 2>/dev/null || true)
        case "$phase" in Succeeded | Failed) break ;; esac
        sleep 2
    done

    local out
    out=$(kubectl -n "$NS" logs "$pod" 2>&1 || true)

    case "$expect" in
    served)
        if [ "$out" = "200" ]; then
            echo "    $who: served ($out)"
        else
            echo "IDENTITY: $who holds an admitted identity and was not served: $out" >&2
            exit 1
        fi
        ;;
    refused)
        # A bare refusal, and that is deliberate: telling a caller WHICH rule
        # rejected it describes the allow-list to whoever is probing. The
        # reason is on the server.
        if [ "$out" = "200" ]; then
            echo "IDENTITY: $who is not on the list and was served anyway" >&2
            echo "          the allow-list is not being enforced" >&2
            exit 1
        fi

        echo "    $who: refused"
        ;;
    esac
}

echo "==> the caller on the list"
probe admitted served

echo "==> a caller with a real identity that is not on the list"
probe stranger refused

# The caller was told nothing; the SERVICE is where the reason lives. Without
# this check a service refusing everyone for an unrelated reason passes the
# test above and looks correct.
echo "==> what the service said about the refusal"
said=$(kubectl -n "$NS" logs -l app.kubernetes.io/component=redirect --tail=60 --all-containers 2>/dev/null |
    grep -m1 '"msg":"refused a peer"' || true)

if [ -z "$said" ]; then
    echo "IDENTITY: the stranger was refused and the service never said why" >&2
    echo "          a refusal nobody can find is one nobody can tell from a broken service" >&2
    exit 1
fi

echo "    $said" | sed 's/^/    /'

if ! grep -q '"serviceAccount":"stranger"' <<<"$said"; then
    echo "IDENTITY: the service logged a refusal, but not of the caller under test" >&2
    exit 1
fi

# The other half of the rule, and the half the probes above cannot reach: a
# CLIENT presenting an identity.
#
# Everything so far tests a server's decision about a caller. But the counter
# calls the URL service, and under `strict` there is no cleartext port to
# fall back to — so if the client did not present its certificate, or the
# server did not admit it, the count simply stops moving. That failure is
# silent from every angle except this one.
echo "==> the whole release on strict, and the counter still counting"
helm upgrade "$APP" "$CHARTS/url-shortener" -n "$NS" --reuse-values \
    --set tls.mode=strict \
    --set "tls.trustDomain=$TRUST_DOMAIN" \
    --wait --timeout 5m >/dev/null

KEY="mtl$(printf '%05d' $((RANDOM % 100000)))"
LONG="https://example.com/strict/$KEY"
ID=$(printf '%s' "$LONG" | sha256sum | cut -d' ' -f1)

kubectl -n "$NS" exec "${INFRA:-infra}-pg-1" -c postgres -- \
    psql -qtAX -d url_shortener -c \
    "INSERT INTO urls.urls (id, url_key, long_url, created_at)
     VALUES ('$ID', '$KEY', '$LONG', now()) ON CONFLICT (id) DO NOTHING;" >/dev/null

# Published straight to the stream rather than driven through the redirect
# service. Under `strict` that service REQUIRES a client certificate, so
# nothing outside the mesh of identities can call it — which is the rule
# working, and also why this step cannot use curl.
kubectl -n nats exec deploy/nats-box -- nats --server nats://nats:4222 \
    pub "$NS-$INSTALL_NAME.redirect" \
    "{\"url_key\":\"$KEY\",\"long_url\":\"$LONG\",\"timestamp\":\"2026-01-01T00:00:00Z\"}" \
    -H X-Detail-Type:URLRedirect >/dev/null

count=0
for _ in $(seq 1 30); do
    count=$(kubectl -n "$NS" exec "${INFRA:-infra}-pg-1" -c postgres -- \
        psql -qtAX -d url_shortener -c \
        "SELECT click_count FROM stats.stats WHERE id = '$ID';" | tr -d '[:space:]')
    [ "${count:-0}" -ge 1 ] 2>/dev/null && break
    sleep 2
done

if [ "${count:-0}" -lt 1 ]; then
    echo "IDENTITY: the counter never reached the URL service over mutual TLS" >&2
    echo "          a client that does not present its identity fails exactly here, silently" >&2
    kubectl -n "$NS" logs -l app.kubernetes.io/component=stat --tail=20 >&2
    kubectl -n "$NS" logs -l app.kubernetes.io/component=urls --tail=20 >&2
    exit 1
fi
echo "    the counter reached it and the count moved to $count"

# The box is left as it was found. `strict` takes the cleartext port away,
# so anything that port-forwards — the other smoke test, a person having a
# look — would find a service that refuses them and no obvious reason why.
echo "==> putting the transport back"
helm upgrade "$APP" "$CHARTS/url-shortener" -n "$NS" --reuse-values \
    --set tls.mode=off --wait --timeout 5m >/dev/null

echo "identity smoke passed: the platform attested it, the service checked it, the stranger was closed, and a client presented its own"
