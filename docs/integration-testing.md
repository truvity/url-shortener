# Integration testing

How the end-to-end suite runs, what it runs against, and why each piece is the
shape it is. The unit and chart tests (`just check`) need no cluster; this is
the part that does.

## The shape: two doors

The test process touches a release through exactly two doors and nothing else:

1. **The Kubernetes API**, through a kubeconfig whose credential is
   `sluisctl kube-token` (audience `k8s:devel`): helm install and uninstall in
   this repository's CI namespace, rollout status, pod logs.
2. **The release's public host**, `https://<release>.<installs domain>`, with a
   pseudo-customer token for audience `devel:url-shortener`
   (`sluisctl token`, a second exchange). The host sits behind a gateway JWT
   policy that admits that audience.

There is no ClusterIP, no database DSN, no bucket and no in-cluster DNS name in
the test process. A suite that can only do what a customer can do cannot pass
because of something a customer cannot reach, and it runs from anywhere that
holds the two credentials: a GitHub-hosted runner, or a laptop after
`sluisctl login`.

The public host reaches the **front end** (`web`) and the **resolver**
(`redirect`). The URL service has no route, so the suite creates links through
the front end's `POST /api/urls` (the call the page makes) and reads the click
count through `GET /api/url`; `stat` and `log` have neither a Service nor a
route and are covered by rollout status and, for the archive, the archiver's
own log (it writes `archived` only after the object was put).

| Test | Door |
|---|---|
| create (generated key, idempotent), redirect 302, counter moves exactly once, front end lists | public host |
| every Deployment is ready; the archiver wrote an object under this release's prefix | Kubernetes API |
| the migration created the tables, the owner owns them, the runtime role cannot issue DDL | none: a local PostgreSQL (`internal/migration`) |

## What runs where

| Where | What |
|---|---|
| any machine, `just check` | Go, Python and Kotlin unit tests; chart renders and goldens (the library chart is pulled from ghcr.io); lint; generated-code drift; the leak canary |
| `internal/migration`, `internal/pgtest` | a PostgreSQL started from `TestMain`: the devbox `postgresql_18` package, a private data directory, `127.0.0.1` on a free port, `trust` authentication. No container. `TEST_DATABASE_URL` replaces the server |
| `.github/workflows/integration.yaml` | the integration suite, on pushes to `master`, release tags and manual dispatch of `master` only |

## The install

`e2e/cmd/e2ectl install` does, through the Kubernetes API:

1. creates the ServiceAccounts the charts would (the archiver runs as the
   namespace's standing account that has the Pod Identity association, so the
   charts are told not to render any) and the runtime role's credential, which
   the charts take by name;
2. installs `url-shortener-infra` (the database, the stream), waits for the
   database;
3. publishes the database's own certificate authority as the ConfigMap the
   services verify the server against (`verify-full`, never weaker);
4. installs `url-shortener`; its pre-install hook migrates the schema in the
   cluster.

The images are built on the runner (`hack/snapshot.sh`: GoReleaser, exactly as a
release) and pushed to the repository's ghcr.io packages as `ci-<sha>`; the
charts are packaged by `helmctl` from what was pushed and installed from the
file, never pushed. `e2ectl uninstall` runs under `if: always()`, and the next
run sweeps what a killed one left.

## No estate particulars in this repository

The repository is public and its leak canary refuses the estate's host names.
The cluster's API endpoint and certificate authority, the access issuer, the
installs domain and the shared test bucket therefore arrive as repository
variables (`DEVEL_API_SERVER`, `DEVEL_API_CA`, `SLUIS_ISSUER`,
`E2E_INSTALLS_DOMAIN`, `E2E_ARCHIVE_BUCKET`, `E2E_ARCHIVE_REGION`), and `hack/kubeconfig.sh` writes
the kubeconfig at run time.

## Who can start it

Nobody from outside. The integration workflow has no `pull_request` trigger; a
pull request from a fork runs `ci.yaml` only (a read-only token, no secret, no
OIDC token: GitHub withholds the id-token from fork runs). The cluster's side is
a pinned group that admits only a push of `master`, a push of a release tag, or
a dispatch of `master`, each from `integration.yaml`, for this repository
alone. Changes to the workflows, the Justfile, the release configuration and
`hack/` need a code owner. Same-repository branches join the integration lane
once the pinned group is proven in use.
