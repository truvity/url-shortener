#!/usr/bin/env bash
# Writes the kubeconfig the integration suite's first door uses: the devel
# cluster, authenticated by `sluisctl kube-token` (audience k8s:devel), which
# answers from a `sluisctl login` session on a laptop and exchanges the job's
# GitHub OIDC token in a workflow.
#
# The cluster's endpoint and certificate authority are not in this repository
# (it is public, and its leak canary refuses the estate's host names): they
# arrive as DEVEL_API_SERVER and DEVEL_API_CA (the CA, base64, as in a
# kubeconfig), the access issuer as SLUIS_ISSUER. The workflow takes all
# three from repository variables.
#
#   hack/kubeconfig.sh [path]     default: $KUBECONFIG, else ./kubeconfig
set -euo pipefail

: "${DEVEL_API_SERVER:?not set: the API endpoint of the cluster}"
: "${DEVEL_API_CA:?not set: the certificate authority of the cluster, base64}"
: "${SLUIS_ISSUER:?not set: the URL of the access issuer}"

out="${1:-${KUBECONFIG:-kubeconfig}}"
umask 077

cat >"$out" <<YAML
apiVersion: v1
kind: Config
current-context: devel@oidc
clusters:
- name: devel
  cluster:
    certificate-authority-data: ${DEVEL_API_CA}
    server: ${DEVEL_API_SERVER}
contexts:
- name: devel@oidc
  context:
    cluster: devel
    user: devel@oidc
users:
- name: devel@oidc
  user:
    exec:
      apiVersion: client.authentication.k8s.io/v1beta1
      command: sluisctl
      args:
      - kube-token
      - --issuer=${SLUIS_ISSUER}
      - --audience=k8s:devel
      interactiveMode: Never
      provideClusterInfo: false
YAML

echo "wrote $out" >&2
