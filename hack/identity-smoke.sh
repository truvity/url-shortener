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

NS=${NS:-shortener}
APP=${APP:-example}
TRUST_DOMAIN=${TRUST_DOMAIN:-policy.local}
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

echo "identity smoke passed: the platform attested it, the service checked it, and the stranger was closed"
