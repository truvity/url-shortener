<!-- Generated from a contract by contracts.docs. Do not edit. -->

# Contract reference

## Documents

| Document | Title | `$id` |
|---|---|---|
| `echo` | echo | `https://example.com/service-example/schemas/echo.json` |
| `fragments/bucket` | bucket | `https://github.com/truvity/policy/schemas/fragments/bucket.json` |
| `fragments/drain` | drain | `https://github.com/truvity/policy/schemas/fragments/drain.json` |
| `fragments/listen` | listen | `https://github.com/truvity/policy/schemas/fragments/listen.json` |
| `fragments/log` | log | `https://github.com/truvity/policy/schemas/fragments/log.json` |
| `fragments/nats` | nats | `https://github.com/truvity/policy/schemas/fragments/nats.json` |
| `fragments/nats-consumer` | nats-consumer | `https://github.com/truvity/policy/schemas/fragments/nats-consumer.json` |
| `fragments/platform` | platform | `https://github.com/truvity/policy/schemas/fragments/platform.json` |
| `fragments/postgres` | postgres | `https://github.com/truvity/policy/schemas/fragments/postgres.json` |
| `fragments/probes` | probes | `https://github.com/truvity/policy/schemas/fragments/probes.json` |
| `fragments/tls` | tls | `https://github.com/truvity/policy/schemas/fragments/tls.json` |
| `log` | log | `https://example.com/url-shortener/schemas/log.json` |
| `migrate` | migrate | `https://example.com/url-shortener/schemas/migrate.json` |
| `prober` | prober | `https://example.com/url-shortener/schemas/prober.json` |
| `redirect` | redirect | `https://example.com/url-shortener/schemas/redirect.json` |
| `service` | service | `https://github.com/truvity/policy/schemas/service.json` |
| `stat` | stat | `https://example.com/url-shortener/schemas/stat.json` |
| `urls` | urls | `https://example.com/url-shortener/schemas/urls.json` |
| `web` | web | `https://example.com/url-shortener/schemas/web.json` |

## Types

The constrained types the fields use. A pattern is a search, and never matches a string with a line break in it.

| Type | Kind | Constraints | Description |
|---|---|---|---|
| `HostPort` | string | matches `^[^\t \xA0  -   　]*:[0-9]{1,5}$`, never a line break | host:port as the configuration contract spells it today. It does NOT bound the port at 65535, because the hand-written pattern does not; `Port` is the stricter vocabulary a contract may move to. |
| `TlsMode` | one of `off`, `permissive`, `strict` |  | How a workload's transport is authenticated. |
| `NonEmptyString` | string | at least 1 character | A string of at least one character. |
| `EnvName` | string | matches `^[A-Za-z_][A-Za-z0-9_]*$`, never a line break | The name of an environment variable. |
| `LogLevel` | one of `debug`, `info`, `warn`, `error` |  | The lowest level a service writes. |
| `PositiveInt` | integer | at least 1 | A whole number of at least one. |
| `Url` | string | format `uri` | An RFC 3986 URI. `format: uri` is an annotation here, never asserted, so the alias asserts nothing either. |
| `ImageDigest` | string | matches `^(sha256:[0-9a-f]{64})?$`, never a line break | An image digest, or empty when there is none. |
| `PullPolicy` | one of `Always`, `IfNotPresent`, `Never` |  | When a container image is pulled. |
| `NonNegativeInt` | integer | at least 0 | A whole number of at least zero. |
| `OpenObject` | open |  | Kubernetes' own object shape, passed through unchanged. |
| `RootedPath` | string | matches `^/`, never a line break | A path that starts at the root, the root itself included. |
| `AbsPath` | string | matches `^/[^\n]+`, never a line break | An absolute path that is not the root. |
| `OtelProtocol` | one of `grpc`, `http/protobuf`, `http/json` |  | The protocol OpenTelemetry exports over. |
| `Named` | open | has the keys `name` | An open object that must carry `name`. |
| `Mounted` | open | has the keys `name`, `mountPath` | An open object that must carry `name` and `mountPath`. |
| `PostgresUrl` | string | matches `^postgres(ql)?://`, never a line break | A PostgreSQL connection URL, by its scheme. |
| `GoDuration` | string | matches `^[0-9]+(ns\|us\|µs\|ms\|s\|m\|h)$`, never a line break | A duration in Go's spelling, as a string: `10s`, `500ms`. Pkl's own `Duration` renders as an object, which no other language reads. |
| `CspMode` | one of `off`, `report-only`, `enforce` |  | What a page's content security policy does. |
| `HttpOrigin` | string | matches `^https?://[A-Za-z0-9.*-]+(:[0-9]+)?$`, never a line break | A bare web origin: scheme, host and an optional port, never a path. |
| `ReportUri` | string | matches `^[^\t \xA0  -   　;,'"]*$`, never a line break | Where a browser reports a violation: empty, a path or an absolute URL, with nothing that would end a directive. |
| `CollectorUrl` | string | matches `^(https://[^\t \xA0  -   　'"<>;,]+\|/[^\t \xA0  -   　'"<>;,?#]*)$`, never a line break | Where a page posts telemetry: a path on its own origin, or an absolute HTTPS URL. |
| `PublicKey` | string | matches `^[A-Za-z0-9._~-]*$`, never a line break | A public identifier: URL-safe characters only. Not a secret. |
| `Ratio` | number | from 0 to 1 | A fraction, zero to one. |

## Classes

### `ServiceConfig`

The envelope every service's configuration carries. A service extends this
module and adds its own properties beside it:

    extends "package://.../contracts.templates@<version>#/ServiceConfig.pkl"

What is in here is what EVERY component has, including a job that exits and
a consumer that answers nothing: somewhere to report health, a log level,
and a shutdown budget. A listener is not one of those, so it is not here.

The module is open so that a service can extend it, and its classes are
closed, so a key that is not declared is refused: a typo must fail.

Document `https://github.com/truvity/policy/schemas/service.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `probes` | Probes | yes |  |  | The health listener. Required: every component can be probed. |  |
| `log` | Log | no |  |  | Structured logging. Absent means the service's own default level. |  |
| `drain` | Drain | no |  |  | The shutdown budget. Absent means the service's own default, which is only correct if nothing external is counting. |  |

### `Echo`

A service that answers with a greeting.

Not a url-shortener component: it is the service of the chart in
`charts/testdata/service-example`, the smallest chart that follows the library
convention (docs/guides/charts.md), and is modelled here because that chart's
values are exactly the two blocks `ChartValues` is made of.

Extends `ServiceConfig`.

Document `https://example.com/service-example/schemas/echo.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `listen` | Listen | yes |  |  |  |  |
| `tls` | Tls | no |  | when `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required |  |  |
| `greeting` | NonEmptyString | yes |  | at least 1 character |  |  |
| `store` | EchoStore | no |  |  | A store the service reads. The token is a SECRET, so the file names the environment variable that holds it and the chart declares that variable in `platform.secrets`. |  |

### `Listen`

A TCP listener. `address` is a host:port the service binds; an empty host binds every interface.

Document `https://github.com/truvity/policy/schemas/fragments/listen.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `address` | HostPort | yes |  | matches `^[^\t \xA0  -   　]*:[0-9]{1,5}$`, never a line break | host:port, for example ":8080" or "127.0.0.1:8080". |  |

### `TlsFields`

Mutually authenticated transport, where the platform provides the identity. A workload presents a certificate it did not mint, reloads it without restarting, and admits peers by the account they run as rather than by the address they call from. Absent, or mode 'off', means cleartext: a service must be installable on a platform that provides none of this.

Document `https://github.com/truvity/policy/schemas/fragments/tls.json`. When `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `mode` | TlsMode | no | `"off"` |  | 'off' serves cleartext only. 'permissive' serves BOTH, on two ports, so that an edge can migrate one side at a time without a coordinated window. 'strict' serves only the authenticated port. One listener cannot be both in every runtime, which is why permissive is two ports rather than one that sniffs. |  |
| `address` | NonEmptyString | no |  | at least 1 character | Where the authenticated listener binds under 'permissive', beside the cleartext one. Under 'strict' there is one listener and it is the service's own, so this is unused: the protocol changes, the address does not, and nothing downstream has to be told. |  |
| `certFile` | NonEmptyString | no |  | at least 1 character | The certificate this workload presents, mounted and rotated by the platform. Re-read when it changes, never cached for the process's lifetime: a one-hour certificate outlives no deployment. |  |
| `keyFile` | NonEmptyString | no |  | at least 1 character | Its private key. It lives in the pod and never in a secret, so a workload that can read secrets in its namespace still cannot read a neighbour's key. |  |
| `caFile` | NonEmptyString | no |  | at least 1 character | The authority peers are verified against, distributed by the platform as a trust bundle. |  |
| `trustDomain` | NonEmptyString | no |  | at least 1 character | The root of every identity this service will admit, for example 'example.internal'. A peer whose identity belongs to another trust domain is refused before its account is even considered. |  |
| `peers` | list of TlsPeer | no |  |  | Who may call. Each entry is an ACCOUNT, not an address: an address resolves to whoever holds it today. An empty list admits no one, which is the correct default for a service nobody has been granted. |  |

### `TlsPeer`

One account that may call: the namespace it runs in and its ServiceAccount name.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `namespace` | NonEmptyString | yes |  | at least 1 character | The namespace the account lives in. |  |
| `serviceAccount` | NonEmptyString | yes |  | at least 1 character | The ServiceAccount the peer runs as. |  |

### `EchoStore`

A store the service reads. The token is a SECRET, so the file names the environment variable that holds it and the chart declares that variable in `platform.secrets`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `tokenEnv` | EnvName | yes |  | matches `^[A-Za-z_][A-Za-z0-9_]*$`, never a line break | **Names a secret.**  |  |

### `Probes`

The health listener. Separate from the service's own traffic, so that readiness is answerable when the service's listener is saturated, and so that a probe is not reachable from outside.

Document `https://github.com/truvity/policy/schemas/fragments/probes.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `address` | HostPort | yes |  | matches `^[^\t \xA0  -   　]*:[0-9]{1,5}$`, never a line break | host:port for /health/live and /health/ready. |  |

### `Log`

Structured logging. One level for the whole service: per-package levels are deliberately not part of the contract.

Document `https://github.com/truvity/policy/schemas/fragments/log.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `level` | LogLevel | no | `"info"` |  | The lowest level that is written. |  |

### `Drain`

How long the service may take to finish in-flight work after SIGTERM. It is one number shared with whatever deploys the service: the grace period granted to the process and the pre-stop delay before it are derived from this, so that a draining process is never killed at the moment it would have finished.

Document `https://github.com/truvity/policy/schemas/fragments/drain.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `seconds` | PositiveInt | no | `20` | at least 1 | Seconds to finish in-flight work. Unset means the service's own default, which is only correct if nothing external is counting. |  |

### `Bucket`

An object store addressed by the S3 API. The vendor is not part of the configuration: an endpoint, a region and a path-style flag are enough to reach a cloud service, an in-cluster store or a test double, and a service that hard-codes one of them cannot be tested without it.

Document `https://github.com/truvity/policy/schemas/fragments/bucket.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `name` | NonEmptyString | yes |  | at least 1 character | The bucket. It exists already: a service does not create its own store. |  |
| `region` | NonEmptyString | no |  | at least 1 character | The region the bucket is in. |  |
| `endpoint` | Url | no |  | format `uri` | Override the API endpoint. Unset means the SDK's own resolution for the region. |  |
| `ca` | NonEmptyString | no |  | at least 1 character | Path to a certificate authority bundle the platform mounts, for a store whose endpoint is not signed by a public root. A store inside somebody's own network is the ordinary case, not the exotic one. Unset means the system trust store. |  |
| `pathStyle` | boolean | no | `false` |  | Address the bucket as a path rather than as a host. Required by most non-cloud implementations. |  |
| `credentialsEnv` | BucketCredentialsEnv | no |  |  | **Names a secret.** The NAMES of the environment variables holding the credentials, never the values. Unset means the SDK's ambient credentials, which is what a workload identity provides. |  |

### `BucketCredentialsEnv`

The NAMES of the environment variables holding the credentials, never the values. Unset means the SDK's ambient credentials, which is what a workload identity provides.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `accessKeyID` | NonEmptyString | yes |  | at least 1 character | The name of the variable holding the access key ID. |  |
| `secretAccessKey` | NonEmptyString | yes |  | at least 1 character | The name of the variable holding the secret access key. |  |

### `Nats`

A NATS connection, and nothing else. What a service does with the connection — publish to a subject, bind a durable consumer to a stream — differs per component and is described beside it: a publisher with a `consumer` field it never reads is a field somebody will eventually set.

Document `https://github.com/truvity/policy/schemas/fragments/nats.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `url` | NonEmptyString | yes |  | at least 1 character | The server URL, for example nats://nats:4222. |  |
| `tokenFile` | NonEmptyString | no |  | at least 1 character | Path to a file holding the token the client authenticates with, mounted by the platform and re-read on every reconnect so that a rotated token is picked up without a restart. It is the workload's own account token: the broker asks an authorisation service who the bearer is, and that service answers from the account rather than from anything the client claims. Unset with `tls` unset means no authentication, which is a test configuration and not a deployment; unset with `tls` set means the certificate is the credential. |  |
| `tls` | NatsTls | no |  |  | Connect over TLS and authenticate with the service's own workload identity instead of a token. The certificate presented is the one the service's own top-level `tls` block loads (its mode must not be `off`); the broker maps the identity in it to a user with its own permissions, so what the service may do is the broker's decision and nothing in this file. The certificate is re-read on every connect, so the platform's rotation is picked up without a restart and a rotation drops no connection already made. Only a service that declares this key in its own schema acts on it. |  |

### `NatsTls`

Connect over TLS and authenticate with the service's own workload identity instead of a token. The certificate presented is the one the service's own top-level `tls` block loads (its mode must not be `off`); the broker maps the identity in it to a user with its own permissions, so what the service may do is the broker's decision and nothing in this file. The certificate is re-read on every connect, so the platform's rotation is picked up without a restart and a rotation drops no connection already made. Only a service that declares this key in its own schema acts on it.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `caFile` | NonEmptyString | yes |  | at least 1 character | Path to the trust bundle the BROKER's certificate is verified against. The broker's, not the workload identity's: a broker has a name and no workload identity, and usually a different chain. |  |
| `serverName` | string | no |  |  | The name the broker's certificate was issued for, when that is not the host in `url`. Empty verifies the host dialled. |  |

### `NatsConsumer`

What a consumer binds to: a stream, a durable name, and the subject it filters. Durable by name, because a consumer that forgets its position on restart replays or loses whatever arrived while it was gone.

Document `https://github.com/truvity/policy/schemas/fragments/nats-consumer.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `stream` | NonEmptyString | yes |  | at least 1 character | The stream to consume from. It exists already: a service does not create the stream it reads. |  |
| `durable` | NonEmptyString | yes |  | at least 1 character | The durable consumer name. Shared by every replica of this component, which is what makes them one consumer group rather than several. |  |
| `subject` | NonEmptyString | no |  | at least 1 character | Filter the stream to this subject. Unset consumes everything the stream holds. |  |

### `Platform`

What the platform provides one component of a service chart: the image, the replicas, the account it runs as, how it is probed, where its identity is mounted, which secrets reach it as environment variables, and how it exports telemetry. The shape of the `platform` block of a chart's values, read by the library chart (decision 0009 of the policy repository). Nothing here is the service's own configuration: that is the chart's `config` block, which is the service's schema and nothing else.

Document `https://github.com/truvity/policy/schemas/fragments/platform.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `image` | PlatformImage | no |  |  | Where the image is. A digest when there is one; a tag only when there is not. Left out, the library reads the chart's own top-level `images.<component>`, the map a release stamps and refuses to publish with an empty digest. |  |
| `imagePullPolicy` | PullPolicy | no |  |  | Defaults to IfNotPresent. |  |
| `replicas` | NonNegativeInt | no |  | at least 0 | Defaults to 1. A service that must survive a rollout runs more than one: one instance cannot be replaced without a gap whatever the strategy says. |  |
| `strategy` | PlatformStrategy | no |  |  | The rolling update. The defaults make a rollout gapless: the replacement is READY before the incumbent is touched. |  |
| `resources` | OpenObject | no |  |  | Kubernetes' own resource requirements. Open: it is passed through unchanged. |  |
| `podSecurity` | PlatformPodSecurity | no |  |  | Who the process is. Every field defaults to 65532, the unprivileged user the runtime images run as; `fsGroup` is the one that matters, because a CSI driver writes what it mounts owned by root. |  |
| `serviceAccount` | PlatformServiceAccount | no |  |  | The account this component runs as. EVERY component has its own, always; `default` is refused. The library grants nothing: annotations are where a platform binds the account to rights outside the cluster, and which mechanism does that is the platform's. |  |
| `service` | PlatformService | no |  |  | A component that listens has a Service on the ports of its own `config.listen`; one that does not has none. |  |
| `probes` | PlatformProbes | no |  |  | How the component is probed, on the listener its own `config.probes` binds. Liveness is nothing but the process: a probe that checks a dependency restarts a healthy process and makes an outage worse. |  |
| `drain` | PlatformDrain | no |  |  | The service's own shutdown budget is `config.drain.seconds`; the library derives the grace period from it and this delay, so the three numbers cannot disagree. |  |
| `tls` | PlatformTls | no |  |  | Where the platform mounts the workload identity. The files the component's own `config.tls` names must be under `mountPath`; the render refuses a file that is not. |  |
| `telemetry` | PlatformTelemetry | no |  |  | OpenTelemetry's own environment variables (decision 0006 of the policy repository): they leave the chart as variables, never as configuration keys. No endpoint means do not export. |  |
| `secrets` | map of PlatformSecret | no |  |  | **Names a secret.** The environment variables that carry SECRETS, and nothing else (decision 0002 of the policy repository): variable name to the Secret and key its value comes from. The configuration file names the VARIABLE; the value never appears in a values file or a render. |  |
| `env` | list of PlatformEnvVar | no |  |  | Environment a platform CLIENT LIBRARY reads (a database client's connection variables, for example), never the service's own configuration: a service takes no other structural input than its file (decision 0002 of the policy repository). A secret does not belong here; declare it in `secrets`. |  |
| `volumes` | list of Named | no |  |  | Extra pod volumes, in Kubernetes' own shape, for what a client library mounts (a trust bundle, a password file). Open: passed through unchanged. |  |
| `volumeMounts` | list of Mounted | no |  |  | The mounts for `volumes`, in Kubernetes' own shape. |  |
| `config` | PlatformConfig | no |  |  | How the file reaches the process. The path is ONE argument or ONE environment variable, never both (decision 0002 of the policy repository). |  |
| `configMap` | PlatformConfigMap | no |  |  | The ConfigMap the file is rendered into. |  |

### `PlatformImage`

Where the image is. A digest when there is one; a tag only when there is not. Left out, the library reads the chart's own top-level `images.<component>`, the map a release stamps and refuses to publish with an empty digest.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `registry` | string | no |  |  | The registry host. Left out, the repository is read as the whole name. |  |
| `repository` | NonEmptyString | yes |  | at least 1 character | The repository path, without the registry and without a tag. |  |
| `tag` | string | no |  |  | The tag. Empty or absent when there is a digest. |  |
| `digest` | ImageDigest | no |  | matches `^(sha256:[0-9a-f]{64})?$`, never a line break | The content digest, `sha256:` and 64 hex digits; empty when there is none. |  |

### `PlatformStrategy`

The rolling update. The defaults make a rollout gapless: the replacement is READY before the incumbent is touched.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `maxUnavailable` | integer \| string | no |  |  | Defaults to 0. |  |
| `maxSurge` | integer \| string | no |  |  | Defaults to 1. |  |

### `PlatformPodSecurity`

Who the process is. Every field defaults to 65532, the unprivileged user the runtime images run as; `fsGroup` is the one that matters, because a CSI driver writes what it mounts owned by root.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `runAsUser` | PositiveInt | no |  | at least 1 | The user ID the process runs as. Never zero. |  |
| `runAsGroup` | PositiveInt | no |  | at least 1 | The group ID the process runs as. Never zero. |  |
| `fsGroup` | PositiveInt | no |  | at least 1 | The group that owns what a CSI driver mounts. Never zero. |  |

### `PlatformServiceAccount`

The account this component runs as. EVERY component has its own, always; `default` is refused. The library grants nothing: annotations are where a platform binds the account to rights outside the cluster, and which mechanism does that is the platform's.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `create` | boolean | no |  |  | False where the platform creates the accounts; they must then exist. Defaults to true. |  |
| `name` | NonEmptyString | no |  | at least 1 character | Defaults to `<release>-<component>`. |  |
| `annotations` | map of string | no |  |  | Annotations put on the account, where a platform binds it to rights outside the cluster. |  |

### `PlatformService`

A component that listens has a Service on the ports of its own `config.listen`; one that does not has none.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `enabled` | boolean | no |  |  | Defaults to whether `config.listen` exists. Enabling one for a component that listens on nothing is refused. |  |

### `PlatformProbes`

How the component is probed, on the listener its own `config.probes` binds. Liveness is nothing but the process: a probe that checks a dependency restarts a healthy process and makes an outage worse.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `liveness` | PlatformProbe | no |  |  | The liveness probe: the process and nothing else. |  |
| `readiness` | PlatformProbe | no |  |  | The readiness probe: this instance can serve now. |  |
| `startup` | PlatformProbe | no |  |  | Absent means no startup probe. Present, it needs at least one field. |  |

### `PlatformProbe`

One probe's timing. Every field has the library's own default.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `path` | RootedPath | no |  | matches `^/`, never a line break | The path on the probe listener. Defaults to the contract's own. |  |
| `periodSeconds` | PositiveInt | no |  | at least 1 | How often to probe, in seconds. |  |
| `initialDelaySeconds` | NonNegativeInt | no |  | at least 0 | How long to wait after the start before the first probe, in seconds. |  |
| `timeoutSeconds` | PositiveInt | no |  | at least 1 | How long one probe may take, in seconds. |  |
| `successThreshold` | PositiveInt | no |  | at least 1 | Consecutive successes that make the probe pass again. |  |
| `failureThreshold` | PositiveInt | no |  | at least 1 | Consecutive failures that make the probe fail. |  |

### `PlatformDrain`

The service's own shutdown budget is `config.drain.seconds`; the library derives the grace period from it and this delay, so the three numbers cannot disagree.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `preStopSeconds` | NonNegativeInt | no |  | at least 0 | Fail readiness, then wait this long before the drain starts, so that whatever routes traffic has removed this endpoint first. Defaults to 5. |  |

### `PlatformTls`

Where the platform mounts the workload identity. The files the component's own `config.tls` names must be under `mountPath`; the render refuses a file that is not.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `csiDriver` | NonEmptyString | no |  | at least 1 character | The driver that mounts the identity. The platform's, so there is no default; required once the identity is mounted. |  |
| `mountPath` | AbsPath | no |  | matches `^/[^\n]+`, never a line break | Defaults to /var/run/identity. |  |
| `mount` | boolean | no |  |  | Defaults to whether `config.tls.mode` is permissive or strict. True mounts the identity into a component that presents none of its own, because the release does; false never mounts it. |  |

### `PlatformTelemetry`

OpenTelemetry's own environment variables (decision 0006 of the policy repository): they leave the chart as variables, never as configuration keys. No endpoint means do not export.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `serviceName` | NonEmptyString | no |  | at least 1 character | Defaults to `<release>-<component>`. |  |
| `endpoint` | string | no |  |  | The collector to export to. Absent means do not export. |  |
| `protocol` | OtelProtocol | no |  |  | The protocol to export over. |  |
| `tracesSampler` | string | no |  |  | OpenTelemetry's `OTEL_TRACES_SAMPLER`. |  |
| `sampleRatio` | string \| number | no |  |  | The sampler's argument: a ratio, as a number or a string. |  |
| `resourceAttributes` | map of string | no |  |  | Attributes that describe the resource, name to value. |  |

### `PlatformSecret`

Where an environment variable's value comes from: a key of a Secret. Never the value.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `secretName` | NonEmptyString | yes |  | at least 1 character | The name of the Secret. |  |
| `key` | NonEmptyString | yes |  | at least 1 character | The key within it. |  |

### `PlatformEnvVar`

A plain environment variable. Never a secret.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `name` | NonEmptyString | yes |  | at least 1 character | The variable's name. |  |
| `value` | string | yes |  |  | Its value. |  |

### `PlatformConfig`

How the file reaches the process. The path is ONE argument or ONE environment variable, never both (decision 0002 of the policy repository).

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `fileName` | NonEmptyString | no |  | at least 1 character | Defaults to `<component>.yaml`. |  |
| `mountPath` | AbsPath | no |  | matches `^/[^\n]+`, never a line break | The directory the ConfigMap is mounted at. Defaults to `/etc/<chart name>`. |  |
| `pathFlag` | NonEmptyString | no |  | at least 1 character | The argument that carries the path. Defaults to `-config`. |  |
| `pathEnv` | NonEmptyString | no |  | at least 1 character | **Names a secret.** When set, the path is passed in this environment variable instead of an argument. |  |

### `PlatformConfigMap`

The ConfigMap the file is rendered into.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `annotations` | map of string | no |  |  | For a ConfigMap that must be a hook resource: a pre-install job cannot mount one the release has not created yet. |  |

### `Postgres`

A PostgreSQL connection. The URL carries no password: it names the environment variable that does.

Document `https://github.com/truvity/policy/schemas/fragments/postgres.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `url` | PostgresUrl | yes |  | matches `^postgres(ql)?://`, never a line break | A connection URL without credentials, for example postgres://user@host:5432/dbname?sslmode=require. |  |
| `passwordEnv` | NonEmptyString | no |  | at least 1 character | **Names a secret.** The NAME of the environment variable holding the password. Unset means the connection needs none. |  |
| `maxConnections` | PositiveInt | no | `10` | at least 1 | Pool size for this instance. Sized against the server's limit divided by the number of instances, not guessed. |  |

### `Archiver`

The archiver: consume what the redirect service recorded about each request, and write it to an object store as NDJSON. It owns no database and answers no calls.

Extends `ServiceConfig`.

Document `https://example.com/url-shortener/schemas/log.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `events` | ArchiverEvents | yes |  |  |  |  |
| `archive` | ArchiverArchive | yes |  |  |  |  |

### `ArchiverEvents`

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `nats` | Nats | yes |  |  |  |  |
| `consumer` | NatsConsumer | yes |  |  |  |  |

### `ArchiverArchive`

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `bucket` | Bucket | yes |  |  |  |  |
| `prefix` | string | no | `"url-shortener/requests"` |  | What every key this component writes begins with. A bucket is usually shared, and a component that writes to the root of one cannot be given permission to write only its own objects. |  |
| `batch` | ArchiverBatch | no |  |  | When to close an object and write it. Both limits apply: whichever is reached first. A size-only rule means a quiet hour is never archived; a time-only rule means a busy one is archived in objects too small to be worth reading. |  |

### `ArchiverBatch`

When to close an object and write it. Both limits apply: whichever is reached first. A size-only rule means a quiet hour is never archived; a time-only rule means a busy one is archived in objects too small to be worth reading.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `maxRecords` | PositiveInt | no | `500` | at least 1 | Write once this many records are held. |  |
| `maxSeconds` | PositiveInt | no | `60` | at least 1 | Write this long after the first record of a batch arrived, however few there are. |  |

### `Migrate`

The migration job. It does NOT reference the service envelope: a job that runs once and exits is not a service, and probes it never serves would be configuration a deployment can set and watch do nothing.

Document `https://example.com/url-shortener/schemas/migrate.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `log` | Log | no |  |  |  |  |
| `ownerRole` | NonEmptyString | yes |  | at least 1 character | The role the migration becomes before it creates anything, so that tables are owned by the owner rather than by whoever migrated. Removing the migration user must not orphan the schema. |  |
| `appRole` | NonEmptyString | no |  | at least 1 character | The role the running components use, granted on the app schemas after the migration lands. Unset skips the grant, which is correct where the platform grants it instead. |  |

### `Prober`

Always-on synthetic traffic: walk the SAME journeys a real caller does (create a short link, resolve it, watch its counter move), in a loop, over the two Services this release already serves. Separate from the e2e suite: the suite proves a release IS healthy once; this proves it STAYS healthy, so a bake window has signal to read even when nothing real is happening.

Extends `ServiceConfig`.

Document `https://example.com/url-shortener/schemas/prober.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `interval` | GoDuration | yes |  | matches `^[0-9]+(ns\|us\|µs\|ms\|s\|m\|h)$`, never a line break | How often the loop repeats one full pass of every journey, as a Go duration string, for example "10s". |  |
| `keyPrefix` | NonEmptyString | no |  | at least 1 character | Marks every long URL and key this prober invents, the same role examples/url-shortener/e2e/suite's own testDataPrefix plays for the e2e suite: a person reading the urls table or the archive bucket by hand can tell synthetic traffic from a real caller's at a glance. |  |
| `statSettle` | GoDuration | no |  | matches `^[0-9]+(ns\|us\|µs\|ms\|s\|m\|h)$`, never a line break | How long the "stat" journey keeps watching a link's click count AFTER it first reads exactly 1, as a Go duration string, and fails if it moves again. A consumer that fails to acknowledge a message has it redelivered after its ack wait, so a click counted twice looks right at the first read and wrong one ack wait later; this window has to outlast at least one redelivery, so it defaults to twice the consumer's 30s ack wait ("60s"). "0s" turns the hold off. |  |
| `urls` | ProberUrls | yes |  |  | The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is a platform decision this component does not make for itself. |  |
| `redirect` | ProberRedirect | yes |  |  | The service that resolves a short key. An address and nothing else, on the same terms as `urls` above. |  |
| `tls` | Tls | no |  | when `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required |  |  |

### `ProberUrls`

The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is a platform decision this component does not make for itself.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `address` | NonEmptyString | yes |  | at least 1 character |  |  |

### `ProberRedirect`

The service that resolves a short key. An address and nothing else, on the same terms as `urls` above.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `address` | NonEmptyString | yes |  | at least 1 character |  |  |

### `Redirect`

The redirect service: resolve a short key and emit what happened.

Extends `ServiceConfig`.

Document `https://example.com/url-shortener/schemas/redirect.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `listen` | Listen | yes |  |  |  |  |
| `tls` | Tls | no |  | when `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required |  |  |
| `events` | RedirectEvents | yes |  |  |  |  |

### `RedirectEvents`

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `nats` | Nats | yes |  |  |  |  |
| `redirectSubject` | NonEmptyString | yes |  | at least 1 character | Where a resolved redirect is published. One event kind per subject, so a consumer never has to guess what it decoded. |  |
| `requestSubject` | NonEmptyString | yes |  | at least 1 character | Where the request log is published. A separate subject from the redirect, for the same reason. |  |

### `Stat`

The click counter: consume redirects, and ask the service that owns the table to count them.

Extends `ServiceConfig`.

Document `https://example.com/url-shortener/schemas/stat.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `urls` | StatUrls | yes |  |  | The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business. |  |
| `events` | StatEvents | yes |  |  |  |  |
| `tls` | Tls | no |  | when `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required |  |  |

### `StatUrls`

The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `address` | NonEmptyString | yes |  | at least 1 character |  |  |

### `StatEvents`

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `nats` | Nats | yes |  |  |  |  |
| `consumer` | NatsConsumer | yes |  |  |  |  |

### `Urls`

The service that owns the URL tables. Everything that writes them asks it.

Extends `ServiceConfig`.

Document `https://example.com/url-shortener/schemas/urls.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `listen` | Listen | yes |  |  |  |  |
| `tls` | Tls | no |  | when `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required |  |  |

### `Web`

The front end: serve the page, and ask the service that owns the tables. It writes nothing and holds no database credential.

Extends `ServiceConfig`.

Document `https://example.com/url-shortener/schemas/web.json`.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `listen` | Listen | yes |  |  |  |  |
| `tls` | Tls | no |  | when `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required |  |  |
| `urls` | WebUrls | yes |  |  | The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business. |  |
| `csp` | WebCsp | no |  |  | The Content-Security-Policy this server sends with every response. Absent means report-only with no extra origins: the header is sent, nothing is blocked, so turning it on cannot break a page. |  |
| `faro` | WebFaro | no |  |  | Browser telemetry (Grafana Faro). Absent, or `enabled: false`, means the page sends nothing and the Faro libraries are never downloaded. Every field here is written into the page for every visitor to read: `apiKey` is a PUBLIC identifier of this app at the collector (one key per app, rotatable, paired with the collector's origin allow-list), not a secret, and no credential may be put in this block. |  |
| `assets` | WebAssets | yes |  |  | Where the built page is. A path rather than an embedded bundle, because the assets are the one thing in this image that a CDN might serve instead. |  |

### `WebUrls`

The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `address` | NonEmptyString | yes |  | at least 1 character |  |  |

### `WebCsp`

The Content-Security-Policy this server sends with every response. Absent means report-only with no extra origins: the header is sent, nothing is blocked, so turning it on cannot break a page.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `mode` | CspMode | no | `"report-only"` |  | `report-only` sends Content-Security-Policy-Report-Only: a browser reports a violation to its console (and to reportUri) and blocks nothing. `enforce` sends Content-Security-Policy. `off` sends neither. |  |
| `connectSrc` | list of HttpOrigin | no | `[]` |  | Origins the page may connect to besides its own, added to `connect-src 'self'`. Each is a bare origin, scheme and host and optional port, never a path and never a keyword. |  |
| `reportUri` | ReportUri | no |  | matches `^[^\t \xA0  -   　;,'"]*$`, never a line break | Where a browser POSTs violation reports. A path on this origin or an absolute URL; empty or absent adds no report-uri directive. |  |

### `WebFaro`

Browser telemetry (Grafana Faro). Absent, or `enabled: false`, means the page sends nothing and the Faro libraries are never downloaded. Every field here is written into the page for every visitor to read: `apiKey` is a PUBLIC identifier of this app at the collector (one key per app, rotatable, paired with the collector's origin allow-list), not a secret, and no credential may be put in this block.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `enabled` | boolean | no | `false` |  | Whether the page sends telemetry at all. |  |
| `collectorUrl` | CollectorUrl | no | `"/faro/collect"` | matches `^(https://[^\t \xA0  -   　'"<>;,]+\|/[^\t \xA0  -   　'"<>;,?#]*)$`, never a line break | Where the page POSTs telemetry: a path on its own origin (the default, `/faro/collect`, which the gateway routes to the collector, so connect-src 'self' is enough) or an absolute HTTPS URL, whose origin is then added to connect-src. |  |
| `apiKey` | PublicKey | no |  | matches `^[A-Za-z0-9._~-]*$`, never a line break | The app's public key at the collector, sent as `x-api-key`. A public identifier, not a secret. |  |
| `appName` | NonEmptyString | no | `"url-shortener-web"` | at least 1 character | The app name the collector sees. |  |
| `environment` | string | no |  |  | A label for where this install runs, for example `devel`. |  |
| `sampleRate` | Ratio | no | `1` | from 0 to 1 | The fraction of browser SESSIONS that report anything, 0 to 1. |  |

### `WebAssets`

Where the built page is. A path rather than an embedded bundle, because the assets are the one thing in this image that a CDN might serve instead.

| Field | Type | Required | Default | Constraints | Description | Set at install |
|---|---|---|---|---|---|---|
| `directory` | NonEmptyString | yes |  | at least 1 character |  |  |

## Secrets

These fields hold the NAME of a secret, never its value: the environment variable that holds it, or the Secret and key it comes from.

| Field | Description |
|---|---|
| `EchoStore.tokenEnv` |  |
| `Bucket.credentialsEnv` | The NAMES of the environment variables holding the credentials, never the values. Unset means the SDK's ambient credentials, which is what a workload identity provides. |
| `Platform.secrets` | The environment variables that carry SECRETS, and nothing else (decision 0002 of the policy repository): variable name to the Secret and key its value comes from. The configuration file names the VARIABLE; the value never appears in a values file or a render. |
| `PlatformConfig.pathEnv` | When set, the path is passed in this environment variable instead of an argument. |
| `Postgres.passwordEnv` | The NAME of the environment variable holding the password. Unset means the connection needs none. |
