# url-shortener

The worked example: a real service, held to the contracts in this
repository, so that "the contracts are complete" is something you can run
rather than something this repository claims.

It shortens URLs. Someone asks for `/r/abc12345`, it answers with a redirect
and says what happened; something else counts that.

## What is here

| Component | Kind | Does |
|---|---|---|
| `cmd/migrate` | job, Go | brings the schema up to date, then exits |
| `cmd/urls` | service, Go | **owns the URL tables**; answers Connect, gRPC and gRPC-Web |
| `cmd/redirect` | service, Go | resolves a key, redirects, publishes what happened |
| `stat/` | service, **Kotlin** | consumes redirects, asks `urls` to count them |
| `web/` | service, **TypeScript** | serves the page, asks `urls` what a key points at |
| `log/` | service, **Python** | consumes request records, archives them as NDJSON |

Four languages, one application chart, and the chart does not know which is
which.

**All three shapes are in here on purpose**, because the interesting part of
the rule is which to reach for:

- `stat` → `urls` is an **RPC**, because the table is somebody's property.
  The counter holds an address and no database credential at all.
- `redirect` → `stat` and `log` is an **event**, because a redirect is a fact
  and who cares about it is none of the publisher's business.
- `redirect` → the table is a **direct read**, and it is the deliberate
  exception: the redirect path is the hot path, and a second network hop on
  it is not worth what it buys. It is written down here so the next reader
  does not take it for an oversight.

Three of these are not written in Go, and that is the point of them rather
than a detail. Each reads a configuration file this chart rendered,
validated against a schema it carries itself; each serves the same probes on
the same port; each drains on SIGTERM within the same number the chart gives
the platform; and each deployment is the same [library template](../../charts/service-lib/templates/_workload.tpl)
that never mentions the language. A platform that had to
know which language a workload was written in would be a platform every new
language has to be added to.

## How it satisfies the contracts

Every rule in [the service contract](../../docs/contracts/service.md) is
visible in one place:

- **The graph is in `main`.** Read `cmd/redirect/main.go` top to bottom and
  you know what the process is made of and what it talks to. There is no
  container to ask, and no registration in a package far away.
- **One configuration file per binary**, validated against a schema in
  [`schemas/`](schemas) before anything is constructed. A test asserts each
  type and its schema describe the same fields, which is what stops a
  renamed key becoming a default nobody chose.
- **Secrets are named, not carried.** The database URL has no password in
  it; it names the environment variable that does. There is a test that a
  password written into the file is refused, and that the refusal does not
  repeat it.
- **Probes on their own listener.** Liveness checks nothing; readiness
  checks what the component needs in order to serve.
- **One log level, JSON, on stderr.** stdout is the program's product; a
  service usually has none, and these produce none.
- **SIGTERM drains.** Both servers, and both consumers, which finish the
  messages they already pulled rather than abandoning them to redelivery.
- **One service owns each table.** Everything that writes the URL tables
  asks `urls`; the counter's configuration has no `database` block, and a
  test asserts that it does not — the ownership rule usually shows up as an
  absence rather than as a line of code.
- **A store is an endpoint, not a vendor.** The archiver reaches its bucket
  through a name, a region and a path-style flag, so the same configuration
  shape reaches a cloud service, a store inside the cluster or the local
  box's test double. Nothing in the component knows which it got.
- **Events carry their type in a header**, and the body is the detail. See
  below.

## Two things worth reading the code for

**The publisher interface has one method.** The framework this example
replaced offered four — publish one, publish many, shut down, health-check —
and the service used one. A narrow interface is what lets the tests
substitute five lines instead of a mock, and what stops a caller reaching for
a lifecycle method belonging to whoever opened the connection.

**Events put the type in a header.** The framework's version marshalled only
the detail, so the envelope's type was dropped at publish time. Two kinds of
event on one subject then became indistinguishable, and the counter decoded a
request as a redirect and counted a click for an empty URL. Keeping the body
as the detail keeps every existing consumer working; moving the type to a
header means it cannot silently go missing again.

## The chart is held to the same schema

`charts/url-shortener` renders one configuration file per binary, and
`charts/chart_test.go` pulls each one out of the render and validates it with
**the schema that binary validates against at start-up**. That is the
configuration contract's first rule made real: a key the chart sets and the
binary stopped reading is a test failure in the pull request, not a default
nobody chose in a cluster.

Four negative fixtures in
[`charts/testdata/invalid/`](charts/testdata/invalid), one per refusal: an
unknown key, a route naming no parent, an install that supplies no address
for its database and broker, and a log level the binary would reject. Each
fails for its own reason, checked.

## Browser telemetry

The page can report errors, web vitals and traces to a Grafana Faro collector
(for example Grafana Alloy's `faro.receiver`). It is **off by default**: with no
`faro` block in `web.yaml`, nothing is sent and the Faro libraries are never
downloaded (they sit behind a dynamic import).

The settings are runtime, not build time, because one build is promoted
unchanged from environment to environment. In the chart they are `web.faro`:

```yaml
web:
  faro:
    enabled: true
    collectorUrl: /faro/collect                       # the default: a path on this page's own origin
    apiKey: public-app-key                            # a PUBLIC identifier, not a secret
    appName: url-shortener-web
    environment: devel
    sampleRate: 1                                     # fraction of SESSIONS that report
```

By default the page reports to `/faro/collect` **on its own origin**: the
gateway routes that path to the collector (`route.faro` renders the optional,
off-by-default rule: exact path, POST only, its own rule so the site's sign-in
policy never covers it), which makes the CSP's `connect-src 'self'` enough and
CORS irrelevant. The Node server serves no such path and has no catch-all
page, so nothing here swallows it. An absolute HTTPS `collectorUrl` is also
accepted, and its origin is then added to `connect-src`.

The server writes this block into the page as a JSON data element, so every
value is readable by every visitor: `apiKey` identifies the app to the
collector (pair it with the collector's origin allow-list and rate limit) and
must never be a credential. What leaves the browser: errors, `console.error`
(no other console levels), web vitals, page views, CSP violations, and traces
whose `traceparent` is sent to this page's own origin only, where the server
continues the trace. An anonymous random session id is kept in memory, never
in a cookie or storage. No user is ever set; `meta.user` and identity-named
attributes are dropped; and the query string and fragment are cut from every
URL, with URLs of other origins replaced inside error and log text (a refused
create echoes what the visitor typed). The app version reported is the git
commit the bundle was built from, baked in at build time (`BUILD_ID` overrides
it where there is no checkout), which is also the release a source map is
looked up by.

## Three charts, and the library they render with

| Chart | Installs | Who installs it |
|---|---|---|
| [`url-shortener`](charts/url-shortener) | the six components, their configuration, probes, rollout and route | whoever installs the example |
| [`url-shortener-infra`](charts/url-shortener-infra) | what one install owns: its database and roles, its stream, and — at `tier: primary` — its store and the identity that reaches it | the same caller, once per install, with the same release name |
| [`url-shortener-e2e`](charts/url-shortener-e2e) | the end-to-end suite as a Job, and an optional always-on prober | whoever wants the install proved where it runs |

A fourth, [`service-lib`](../../charts/service-lib), is a **library chart** and is
installed by nobody: it renders what every component of the first chart shares
(a Deployment, a ServiceAccount of its own, a Service, the probes, the mounts,
the telemetry and secret variables) from a `platform` block and a `config`
block, with every port derived from the component's own configuration file. It
lives at the repository root, is released beside the others, and
`url-shortener` resolves it through its `Chart.lock`; [the chart guide](../../docs/guides/charts.md)
says how, and [`charts/testdata/service-example`](charts/testdata/service-example)
is the smallest chart that follows it exactly. `url-shortener` is the one
exception the convention names, a product chart that derives each component's
configuration from the release.

The database and the stream are a **second chart**, not a flag, because the
application's migration runs as a pre-install hook: a chart that created its
own database could never migrate it. [platform.md
§11](../../docs/contracts/platform.md) has the whole argument and places
every resource by scope.

## Running it

From the repository root, with a container runtime:

```sh
just cluster-all   # the box, the release build, the install, and every suite
```

or one step at a time — `just cluster` stands up the box
([`hack/kind/`](../../hack/kind/README.md): servers only, no operator),
`just example-snapshot` builds the images and packages the charts exactly as
a release does, `just example-fixture` provisions what the infrastructure
chart would, `just example-install` installs the packaged application chart,
and `just example-smoke`, `just example-e2e-chart` and `just example-prober`
prove it works. The suite itself is Go, under [`e2e/suite`](e2e/suite).
