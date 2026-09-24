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
| `cmd/stat` | service, Go | consumes redirects, asks `urls` to count them |
| `log/` | service, **Python** | consumes request records, archives them as NDJSON |

A web front end arrives next. The pieces here are the ones that make a
working shortener without a user interface.

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

The archiver is not written in Go, and that is the point of it rather than a
detail. It reads a configuration file this chart rendered, validated against
a schema it carries itself; it serves the same probes on the same port; it
drains on SIGTERM within the same number the chart gives the platform; and
its deployment is [twenty lines that never mention the
language](charts/url-shortener/templates/log.yaml). A platform that had to
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

Five negative fixtures, one per refusal: an unknown key, an image with neither
digest nor tag, a route naming no parent, an install that says it will supply
its own database and then does not, and a log level the binary would reject.
Each fails for its own reason, checked.

The database and the streams are behind `infra.enabled`. An estate that
provisions them separately turns it off and supplies the addresses; a local
cluster turns it on and gets a working install from one command. That is the
same split a two-chart "ring" model expresses, as a flag — with three
components and one database, a second chart would be two files of ceremony
around one resource.

## Running it

```sh
just cluster        # a local Kubernetes with the operators this needs
just cluster-smoke  # prove they act
```

The chart, and the suite that installs this into that cluster and exercises
it, arrive with the next change.
