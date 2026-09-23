# url-shortener

The worked example: a real service, held to the contracts in this
repository, so that "the contracts are complete" is something you can run
rather than something this repository claims.

It shortens URLs. Someone asks for `/r/abc12345`, it answers with a redirect
and says what happened; something else counts that.

## What is here

| Component | Kind | Does |
|---|---|---|
| `cmd/migrate` | job | brings the schema up to date, then exits |
| `cmd/redirect` | service | resolves a key, redirects, publishes what happened |
| `cmd/stat` | service | consumes redirects, counts them |

More arrives: an owning service for the URLs themselves with a typed RPC
boundary, a web front end, and a log archiver. The pieces here are the ones
that make a working shortener without a user interface.

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
- **One log level, JSON, on stdout.**
- **SIGTERM drains.** Both servers, and the consumer, which finishes the
  messages it already pulled rather than abandoning them to redelivery.
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

## Running it

```sh
just cluster        # a local Kubernetes with the operators this needs
just cluster-smoke  # prove they act
```

The chart, and the suite that installs this into that cluster and exercises
it, arrive with the next change.
