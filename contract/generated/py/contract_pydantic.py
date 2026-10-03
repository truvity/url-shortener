# Generated from a contract by contracts.python. Do not edit.
from __future__ import annotations

from typing import Annotated, Any, Literal

from pydantic import (
    AfterValidator,
    BaseModel,
    BeforeValidator,
    ConfigDict,
    Field,
    StringConstraints,
    model_validator,
)


def _no_line_break(value: str) -> str:
    # Every character that ends a line in some engine: LF, CR, FF, VT, NEL, LS, PS.
    if any(c in value for c in "\n\r\f\v\x85\u2028\u2029"):
        raise ValueError("a line break is not allowed")
    return value


def _integral(value: Any) -> Any:
    # An integral float is an integer (20.0 is 20); a bool, a string or a
    # float with a fractional part is not.
    if isinstance(value, float) and value.is_integer():
        return int(value)
    return value


def _at(value: Any, path: list[str]) -> Any:
    # The value at a path through blocks; absent on the way is absent.
    for key in path:
        if value is None:
            return None
        value = getattr(value, key, None)
    return value


def _deny_keys(keys: list[str]):
    def check(value: dict[str, Any]) -> dict[str, Any]:
        found = [k for k in keys if k in value]
        if found:
            raise ValueError(f"must not have the key {found}")
        return value

    return check


def _has_keys(keys: list[str]):
    def check(value: dict[str, Any]) -> dict[str, Any]:
        missing = [k for k in keys if k not in value]
        if missing:
            raise ValueError(f"missing {missing}")
        return value

    return check


class _Closed(BaseModel):
    model_config = ConfigDict(extra="forbid", strict=True)

    @model_validator(mode="before")
    @classmethod
    def _optional_is_absent_not_null(cls, data: Any) -> Any:
        if isinstance(data, dict):
            for key, value in data.items():
                if value is None:
                    raise ValueError(f"{key}: null is not a value; omit the key")
        return data

HostPort = Annotated[str, StringConstraints(pattern=r"^[^\t \xA0  -   　]*:[0-9]{1,5}$"), AfterValidator(_no_line_break)]
TlsMode = Literal["off", "permissive", "strict"]
NonEmptyString = Annotated[str, StringConstraints(min_length=1)]
EnvName = Annotated[str, StringConstraints(pattern=r"^[A-Za-z_][A-Za-z0-9_]*$"), AfterValidator(_no_line_break)]
LogLevel = Literal["debug", "info", "warn", "error"]
PositiveInt = Annotated[int, BeforeValidator(_integral), Field(ge=1)]
Url = str
ImageDigest = Annotated[str, StringConstraints(pattern=r"^(sha256:[0-9a-f]{64})?$"), AfterValidator(_no_line_break)]
PullPolicy = Literal["Always", "IfNotPresent", "Never"]
NonNegativeInt = Annotated[int, BeforeValidator(_integral), Field(ge=0)]
OpenObject = dict[str, Any]
RootedPath = Annotated[str, StringConstraints(pattern=r"^/"), AfterValidator(_no_line_break)]
AbsPath = Annotated[str, StringConstraints(pattern=r"^/[^\n]+"), AfterValidator(_no_line_break)]
OtelProtocol = Literal["grpc", "http/protobuf"]
Named = Annotated[dict[str, Any], AfterValidator(_has_keys(["name"]))]
Mounted = Annotated[dict[str, Any], AfterValidator(_has_keys(["name", "mountPath"]))]
PostgresUrl = Annotated[str, StringConstraints(pattern=r"^postgres(ql)?://"), AfterValidator(_no_line_break)]
GoDuration = Annotated[str, StringConstraints(pattern=r"^[0-9]+(ns|us|µs|ms|s|m|h)$"), AfterValidator(_no_line_break)]
CspMode = Literal["off", "report-only", "enforce"]
HttpOrigin = Annotated[str, StringConstraints(pattern=r"^https?://[A-Za-z0-9.*-]+(:[0-9]+)?$"), AfterValidator(_no_line_break)]
ReportUri = Annotated[str, StringConstraints(pattern=r"^[^\t \xA0  -   　;,'\"]*$"), AfterValidator(_no_line_break)]
CollectorUrl = Annotated[str, StringConstraints(pattern=r"^(https://[^\t \xA0  -   　'\"<>;,]+|/[^\t \xA0  -   　'\"<>;,?#]*)$"), AfterValidator(_no_line_break)]
PublicKey = Annotated[str, StringConstraints(pattern=r"^[A-Za-z0-9._~-]*$"), AfterValidator(_no_line_break)]
Ratio = Annotated[float, Field(ge=0, le=1)]


class ServiceConfig(_Closed):
    """The envelope every service's configuration carries. A service extends this
module and adds its own properties beside it:

    extends "package://.../contracts.templates@<version>#/ServiceConfig.pkl"

What is in here is what EVERY component has, including a job that exits and
a consumer that answers nothing: somewhere to report health, a log level,
and a shutdown budget. A listener is not one of those, so it is not here.

The module is open so that a service can extend it, and its classes are
closed, so a key that is not declared is refused: a typo must fail."""
    model_config = ConfigDict(extra="allow")
    #: The health listener. Required: every component can be probed.
    probes: Probes
    #: Structured logging. Absent means the service's own default level.
    log: Log | None = None
    #: The shutdown budget. Absent means the service's own default, which is only
    #: correct if nothing external is counting.
    drain: Drain | None = None


class Echo(ServiceConfig):
    """A service that answers with a greeting.

Not a url-shortener component: it is the service of the chart in
`charts/testdata/service-example`, the smallest chart that follows the library
convention (docs/guides/charts.md), and is modelled here because that chart's
values are exactly the two blocks `ChartValues` is made of."""
    model_config = ConfigDict(extra="forbid")
    listen: Listen
    tls: Tls | None = None
    greeting: NonEmptyString
    #: A store the service reads. The token is a SECRET, so the file names the environment variable that holds it and the chart declares that variable in `platform.secrets`.
    store: EchoStore | None = None


class Listen(_Closed):
    """A TCP listener. `address` is a host:port the service binds; an empty host binds every interface."""
    #: host:port, for example ":8080" or "127.0.0.1:8080".
    address: HostPort


class TlsFields(_Closed):
    """Mutually authenticated transport, where the platform provides the identity. A workload presents a certificate it did not mint, reloads it without restarting, and admits peers by the account they run as rather than by the address they call from. Absent, or mode 'off', means cleartext: a service must be installable on a platform that provides none of this."""
    #: 'off' serves cleartext only. 'permissive' serves BOTH, on two ports, so that an edge can migrate one side at a time without a coordinated window. 'strict' serves only the authenticated port. One listener cannot be both in every runtime, which is why permissive is two ports rather than one that sniffs.
    mode: TlsMode = Field(default="off")
    #: Where the authenticated listener binds under 'permissive', beside the cleartext one. Under 'strict' there is one listener and it is the service's own, so this is unused: the protocol changes, the address does not, and nothing downstream has to be told.
    address: NonEmptyString | None = None
    #: The certificate this workload presents, mounted and rotated by the platform. Re-read when it changes, never cached for the process's lifetime: a one-hour certificate outlives no deployment.
    certFile: NonEmptyString | None = None
    #: Its private key. It lives in the pod and never in a secret, so a workload that can read secrets in its namespace still cannot read a neighbour's key.
    keyFile: NonEmptyString | None = None
    #: The authority peers are verified against, distributed by the platform as a trust bundle.
    caFile: NonEmptyString | None = None
    #: The root of every identity this service will admit, for example 'example.internal'. A peer whose identity belongs to another trust domain is refused before its account is even considered.
    trustDomain: NonEmptyString | None = None
    #: Who may call. Each entry is an ACCOUNT, not an address: an address resolves to whoever holds it today. An empty list admits no one, which is the correct default for a service nobody has been granted.
    peers: list[TlsPeer] | None = None


class TlsPeer(_Closed):
    """One account that may call: the namespace it runs in and its ServiceAccount name."""
    #: The namespace the account lives in.
    namespace: NonEmptyString
    #: The ServiceAccount the peer runs as.
    serviceAccount: NonEmptyString


class EchoStore(_Closed):
    """A store the service reads. The token is a SECRET, so the file names the environment variable that holds it and the chart declares that variable in `platform.secrets`."""
    tokenEnv: EnvName


class Probes(_Closed):
    """The health listener. Separate from the service's own traffic, so that readiness is answerable when the service's listener is saturated, and so that a probe is not reachable from outside."""
    #: host:port for /health/live and /health/ready.
    address: HostPort


class Log(_Closed):
    """Structured logging. One level for the whole service: per-package levels are deliberately not part of the contract."""
    #: The lowest level that is written.
    level: LogLevel = Field(default="info")


class Drain(_Closed):
    """How long the service may take to finish in-flight work after SIGTERM. It is one number shared with whatever deploys the service: the grace period granted to the process and the pre-stop delay before it are derived from this, so that a draining process is never killed at the moment it would have finished."""
    #: Seconds to finish in-flight work. Unset means the service's own default, which is only correct if nothing external is counting.
    seconds: PositiveInt = Field(default=20)


class Bucket(_Closed):
    """An object store addressed by the S3 API. The vendor is not part of the configuration: an endpoint, a region and a path-style flag are enough to reach a cloud service, an in-cluster store or a test double, and a service that hard-codes one of them cannot be tested without it."""
    #: The bucket. It exists already: a service does not create its own store.
    name: NonEmptyString
    #: The region the bucket is in.
    region: NonEmptyString | None = None
    #: Override the API endpoint. Unset means the SDK's own resolution for the region.
    endpoint: Url | None = None
    #: Path to a certificate authority bundle the platform mounts, for a store whose endpoint is not signed by a public root. A store inside somebody's own network is the ordinary case, not the exotic one. Unset means the system trust store.
    ca: NonEmptyString | None = None
    #: Address the bucket as a path rather than as a host. Required by most non-cloud implementations.
    pathStyle: bool = Field(default=False)
    #: The NAMES of the environment variables holding the credentials, never the values. Unset means the SDK's ambient credentials, which is what a workload identity provides.
    credentialsEnv: BucketCredentialsEnv | None = None


class BucketCredentialsEnv(_Closed):
    """The NAMES of the environment variables holding the credentials, never the values. Unset means the SDK's ambient credentials, which is what a workload identity provides."""
    #: The name of the variable holding the access key ID.
    accessKeyID: NonEmptyString
    #: The name of the variable holding the secret access key.
    secretAccessKey: NonEmptyString


class Nats(_Closed):
    """A NATS connection, and nothing else. What a service does with the connection — publish to a subject, bind a durable consumer to a stream — differs per component and is described beside it: a publisher with a `consumer` field it never reads is a field somebody will eventually set."""
    #: The server URL, for example nats://nats:4222.
    url: NonEmptyString
    #: Path to a file holding the token the client authenticates with, mounted by the platform and re-read on every reconnect so that a rotated token is picked up without a restart. It is the workload's own account token: the broker asks an authorisation service who the bearer is, and that service answers from the account rather than from anything the client claims. Unset with `tls` unset means no authentication, which is a test configuration and not a deployment; unset with `tls` set means the certificate is the credential.
    tokenFile: NonEmptyString | None = None
    #: Connect over TLS and authenticate with the service's own workload identity instead of a token. The certificate presented is the one the service's own top-level `tls` block loads (its mode must not be `off`); the broker maps the identity in it to a user with its own permissions, so what the service may do is the broker's decision and nothing in this file. The certificate is re-read on every connect, so the platform's rotation is picked up without a restart and a rotation drops no connection already made. Only a service that declares this key in its own schema acts on it.
    tls: NatsTls | None = None


class NatsTls(_Closed):
    """Connect over TLS and authenticate with the service's own workload identity instead of a token. The certificate presented is the one the service's own top-level `tls` block loads (its mode must not be `off`); the broker maps the identity in it to a user with its own permissions, so what the service may do is the broker's decision and nothing in this file. The certificate is re-read on every connect, so the platform's rotation is picked up without a restart and a rotation drops no connection already made. Only a service that declares this key in its own schema acts on it."""
    #: Path to the trust bundle the BROKER's certificate is verified against. The broker's, not the workload identity's: a broker has a name and no workload identity, and usually a different chain.
    caFile: NonEmptyString
    #: The name the broker's certificate was issued for, when that is not the host in `url`. Empty verifies the host dialled.
    serverName: str | None = None


class NatsConsumer(_Closed):
    """What a consumer binds to: a stream, a durable name, and the subject it filters. Durable by name, because a consumer that forgets its position on restart replays or loses whatever arrived while it was gone."""
    #: The stream to consume from. It exists already: a service does not create the stream it reads.
    stream: NonEmptyString
    #: The durable consumer name. Shared by every replica of this component, which is what makes them one consumer group rather than several.
    durable: NonEmptyString
    #: Filter the stream to this subject. Unset consumes everything the stream holds.
    subject: NonEmptyString | None = None


class Platform(_Closed):
    """What the platform provides one component of a service chart: the image, the replicas, the account it runs as, how it is probed, where its identity is mounted, which secrets reach it as environment variables, and how it exports telemetry. The shape of the `platform` block of a chart's values, read by the library chart (decision 0009 of the policy repository). Nothing here is the service's own configuration: that is the chart's `config` block, which is the service's schema and nothing else."""
    #: Where the image is. A digest when there is one; a tag only when there is not. Left out, the library reads the chart's own top-level `images.<component>`, the map a release stamps and refuses to publish with an empty digest.
    image: PlatformImage | None = None
    #: Defaults to IfNotPresent.
    imagePullPolicy: PullPolicy | None = None
    #: Defaults to 1. A service that must survive a rollout runs more than one: one instance cannot be replaced without a gap whatever the strategy says.
    replicas: NonNegativeInt | None = None
    #: The rolling update. The defaults make a rollout gapless: the replacement is READY before the incumbent is touched.
    strategy: PlatformStrategy | None = None
    #: Kubernetes' own resource requirements. Open: it is passed through unchanged.
    resources: OpenObject | None = None
    #: Who the process is. Every field defaults to 65532, the unprivileged user the runtime images run as; `fsGroup` is the one that matters, because a CSI driver writes what it mounts owned by root.
    podSecurity: PlatformPodSecurity | None = None
    #: The account this component runs as. EVERY component has its own, always; `default` is refused. The library grants nothing: annotations are where a platform binds the account to rights outside the cluster, and which mechanism does that is the platform's.
    serviceAccount: PlatformServiceAccount | None = None
    #: A component that listens has a Service on the ports of its own `config.listen`; one that does not has none.
    service: PlatformService | None = None
    #: How the component is probed, on the listener its own `config.probes` binds. Liveness is nothing but the process: a probe that checks a dependency restarts a healthy process and makes an outage worse.
    probes: PlatformProbes | None = None
    #: The service's own shutdown budget is `config.drain.seconds`; the library derives the grace period from it and this delay, so the three numbers cannot disagree.
    drain: PlatformDrain | None = None
    #: Where the platform mounts the workload identity. The files the component's own `config.tls` names must be under `mountPath`; the render refuses a file that is not.
    tls: PlatformTls | None = None
    #: OpenTelemetry's own environment variables (decision 0006 of the policy repository): they leave the chart as variables, never as configuration keys. No endpoint means do not export.
    telemetry: PlatformTelemetry | None = None
    #: The environment variables that carry SECRETS, and nothing else (decision 0002 of the policy repository): variable name to the Secret and key its value comes from. The configuration file names the VARIABLE; the value never appears in a values file or a render.
    secrets: dict[EnvName, PlatformSecret] | None = None
    #: Environment a platform CLIENT LIBRARY reads (a database client's connection variables, for example), never the service's own configuration: a service takes no other structural input than its file (decision 0002 of the policy repository). A secret does not belong here; declare it in `secrets`.
    env: list[PlatformEnvVar] | None = None
    #: Extra pod volumes, in Kubernetes' own shape, for what a client library mounts (a trust bundle, a password file). Open: passed through unchanged.
    volumes: list[Named] | None = None
    #: The mounts for `volumes`, in Kubernetes' own shape.
    volumeMounts: list[Mounted] | None = None
    #: How the file reaches the process. The path is ONE argument or ONE environment variable, never both (decision 0002 of the policy repository).
    config: PlatformConfig | None = None
    #: The ConfigMap the file is rendered into.
    configMap: PlatformConfigMap | None = None


class PlatformImage(_Closed):
    """Where the image is. A digest when there is one; a tag only when there is not. Left out, the library reads the chart's own top-level `images.<component>`, the map a release stamps and refuses to publish with an empty digest."""
    #: The registry host. Left out, the repository is read as the whole name.
    registry: str | None = None
    #: The repository path, without the registry and without a tag.
    repository: NonEmptyString
    #: The tag. Empty or absent when there is a digest.
    tag: str | None = None
    #: The content digest, `sha256:` and 64 hex digits; empty when there is none.
    digest: ImageDigest | None = None


class PlatformStrategy(_Closed):
    """The rolling update. The defaults make a rollout gapless: the replacement is READY before the incumbent is touched."""
    #: Defaults to 0.
    maxUnavailable: Annotated[int, BeforeValidator(_integral)] | str | None = None
    #: Defaults to 1.
    maxSurge: Annotated[int, BeforeValidator(_integral)] | str | None = None


class PlatformPodSecurity(_Closed):
    """Who the process is. Every field defaults to 65532, the unprivileged user the runtime images run as; `fsGroup` is the one that matters, because a CSI driver writes what it mounts owned by root."""
    #: The user ID the process runs as. Never zero.
    runAsUser: PositiveInt | None = None
    #: The group ID the process runs as. Never zero.
    runAsGroup: PositiveInt | None = None
    #: The group that owns what a CSI driver mounts. Never zero.
    fsGroup: PositiveInt | None = None


class PlatformServiceAccount(_Closed):
    """The account this component runs as. EVERY component has its own, always; `default` is refused. The library grants nothing: annotations are where a platform binds the account to rights outside the cluster, and which mechanism does that is the platform's."""
    #: False where the platform creates the accounts; they must then exist. Defaults to true.
    create: bool | None = None
    #: Defaults to `<release>-<component>`.
    name: NonEmptyString | None = None
    #: Annotations put on the account, where a platform binds it to rights outside the cluster.
    annotations: dict[str, str] | None = None


class PlatformService(_Closed):
    """A component that listens has a Service on the ports of its own `config.listen`; one that does not has none."""
    #: Defaults to whether `config.listen` exists. Enabling one for a component that listens on nothing is refused.
    enabled: bool | None = None


class PlatformProbes(_Closed):
    """How the component is probed, on the listener its own `config.probes` binds. Liveness is nothing but the process: a probe that checks a dependency restarts a healthy process and makes an outage worse."""
    #: The liveness probe: the process and nothing else.
    liveness: PlatformProbe | None = None
    #: The readiness probe: this instance can serve now.
    readiness: PlatformProbe | None = None
    #: Absent means no startup probe. Present, it needs at least one field.
    startup: PlatformProbe | None = None


class PlatformProbe(_Closed):
    """One probe's timing. Every field has the library's own default."""
    #: The path on the probe listener. Defaults to the contract's own.
    path: RootedPath | None = None
    #: How often to probe, in seconds.
    periodSeconds: PositiveInt | None = None
    #: How long to wait after the start before the first probe, in seconds.
    initialDelaySeconds: NonNegativeInt | None = None
    #: How long one probe may take, in seconds.
    timeoutSeconds: PositiveInt | None = None
    #: Consecutive successes that make the probe pass again.
    successThreshold: PositiveInt | None = None
    #: Consecutive failures that make the probe fail.
    failureThreshold: PositiveInt | None = None


class PlatformDrain(_Closed):
    """The service's own shutdown budget is `config.drain.seconds`; the library derives the grace period from it and this delay, so the three numbers cannot disagree."""
    #: Fail readiness, then wait this long before the drain starts, so that whatever routes traffic has removed this endpoint first. Defaults to 5.
    preStopSeconds: NonNegativeInt | None = None


class PlatformTls(_Closed):
    """Where the platform mounts the workload identity. The files the component's own `config.tls` names must be under `mountPath`; the render refuses a file that is not."""
    #: The driver that mounts the identity. The platform's, so there is no default; required once the identity is mounted.
    csiDriver: NonEmptyString | None = None
    #: Defaults to /var/run/identity.
    mountPath: AbsPath | None = None
    #: Defaults to whether `config.tls.mode` is permissive or strict. True mounts the identity into a component that presents none of its own, because the release does; false never mounts it.
    mount: bool | None = None


class PlatformTelemetry(_Closed):
    """OpenTelemetry's own environment variables (decision 0006 of the policy repository): they leave the chart as variables, never as configuration keys. No endpoint means do not export."""
    #: Defaults to `<release>-<component>`.
    serviceName: NonEmptyString | None = None
    #: The collector to export to. Absent means do not export.
    endpoint: str | None = None
    #: The protocol to export over.
    protocol: OtelProtocol | None = None
    #: OpenTelemetry's `OTEL_TRACES_SAMPLER`.
    tracesSampler: str | None = None
    #: The sampler's argument: a ratio, as a number or a string.
    sampleRatio: str | float | None = None
    #: Attributes that describe the resource, name to value.
    resourceAttributes: dict[str, str] | None = None


class PlatformSecret(_Closed):
    """Where an environment variable's value comes from: a key of a Secret. Never the value."""
    #: The name of the Secret.
    secretName: NonEmptyString
    #: The key within it.
    key: NonEmptyString


class PlatformEnvVar(_Closed):
    """A plain environment variable. Never a secret."""
    #: The variable's name.
    name: NonEmptyString
    #: Its value.
    value: str


class PlatformConfig(_Closed):
    """How the file reaches the process. The path is ONE argument or ONE environment variable, never both (decision 0002 of the policy repository)."""
    #: Defaults to `<component>.yaml`.
    fileName: NonEmptyString | None = None
    #: The directory the ConfigMap is mounted at. Defaults to `/etc/<chart name>`.
    mountPath: AbsPath | None = None
    #: The argument that carries the path. Defaults to `-config`.
    pathFlag: NonEmptyString | None = None
    #: When set, the path is passed in this environment variable instead of an argument.
    pathEnv: NonEmptyString | None = None


class PlatformConfigMap(_Closed):
    """The ConfigMap the file is rendered into."""
    #: For a ConfigMap that must be a hook resource: a pre-install job cannot mount one the release has not created yet.
    annotations: dict[str, str] | None = None


class Postgres(_Closed):
    """A PostgreSQL connection. The URL carries no password: it names the environment variable that does."""
    #: A connection URL without credentials, for example postgres://user@host:5432/dbname?sslmode=require.
    url: PostgresUrl
    #: The NAME of the environment variable holding the password. Unset means the connection needs none.
    passwordEnv: NonEmptyString | None = None
    #: Pool size for this instance. Sized against the server's limit divided by the number of instances, not guessed.
    maxConnections: PositiveInt = Field(default=10)


class Archiver(ServiceConfig):
    """The archiver: consume what the redirect service recorded about each request, and write it to an object store as NDJSON. It owns no database and answers no calls."""
    model_config = ConfigDict(extra="forbid")
    events: ArchiverEvents
    archive: ArchiverArchive


class ArchiverEvents(_Closed):
    nats: Nats
    consumer: NatsConsumer


class ArchiverArchive(_Closed):
    bucket: Bucket
    #: What every key this component writes begins with. A bucket is usually shared, and a component that writes to the root of one cannot be given permission to write only its own objects.
    prefix: str = Field(default="url-shortener/requests")
    #: When to close an object and write it. Both limits apply: whichever is reached first. A size-only rule means a quiet hour is never archived; a time-only rule means a busy one is archived in objects too small to be worth reading.
    batch: ArchiverBatch | None = None


class ArchiverBatch(_Closed):
    """When to close an object and write it. Both limits apply: whichever is reached first. A size-only rule means a quiet hour is never archived; a time-only rule means a busy one is archived in objects too small to be worth reading."""
    #: Write once this many records are held.
    maxRecords: PositiveInt = Field(default=500)
    #: Write this long after the first record of a batch arrived, however few there are.
    maxSeconds: PositiveInt = Field(default=60)


class Migrate(_Closed):
    """The migration job. It does NOT reference the service envelope: a job that runs once and exits is not a service, and probes it never serves would be configuration a deployment can set and watch do nothing."""
    log: Log | None = None
    #: The role the migration becomes before it creates anything, so that tables are owned by the owner rather than by whoever migrated. Removing the migration user must not orphan the schema.
    ownerRole: NonEmptyString
    #: The role the running components use, granted on the app schemas after the migration lands. Unset skips the grant, which is correct where the platform grants it instead.
    appRole: NonEmptyString | None = None


class Prober(ServiceConfig):
    """Always-on synthetic traffic: walk the SAME journeys a real caller does (create a short link, resolve it, watch its counter move), in a loop, over the two Services this release already serves. Separate from the e2e suite: the suite proves a release IS healthy once; this proves it STAYS healthy, so a bake window has signal to read even when nothing real is happening."""
    model_config = ConfigDict(extra="forbid")
    #: How often the loop repeats one full pass of every journey, as a Go duration string, for example "10s".
    interval: GoDuration
    #: Marks every long URL and key this prober invents, the same role examples/url-shortener/e2e/suite's own testDataPrefix plays for the e2e suite: a person reading the urls table or the archive bucket by hand can tell synthetic traffic from a real caller's at a glance.
    keyPrefix: NonEmptyString | None = None
    #: How long the "stat" journey keeps watching a link's click count AFTER it first reads exactly 1, as a Go duration string, and fails if it moves again. A consumer that fails to acknowledge a message has it redelivered after its ack wait, so a click counted twice looks right at the first read and wrong one ack wait later; this window has to outlast at least one redelivery, so it defaults to twice the consumer's 30s ack wait ("60s"). "0s" turns the hold off.
    statSettle: GoDuration | None = None
    #: The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is a platform decision this component does not make for itself.
    urls: ProberUrls
    #: The service that resolves a short key. An address and nothing else, on the same terms as `urls` above.
    redirect: ProberRedirect
    tls: Tls | None = None


class ProberUrls(_Closed):
    """The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is a platform decision this component does not make for itself."""
    address: NonEmptyString


class ProberRedirect(_Closed):
    """The service that resolves a short key. An address and nothing else, on the same terms as `urls` above."""
    address: NonEmptyString


class Redirect(ServiceConfig):
    """The redirect service: resolve a short key and emit what happened."""
    model_config = ConfigDict(extra="forbid")
    listen: Listen
    tls: Tls | None = None
    events: RedirectEvents


class RedirectEvents(_Closed):
    nats: Nats
    #: Where a resolved redirect is published. One event kind per subject, so a consumer never has to guess what it decoded.
    redirectSubject: NonEmptyString
    #: Where the request log is published. A separate subject from the redirect, for the same reason.
    requestSubject: NonEmptyString


class Stat(ServiceConfig):
    """The click counter: consume redirects, and ask the service that owns the table to count them."""
    model_config = ConfigDict(extra="forbid")
    #: The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business.
    urls: StatUrls
    events: StatEvents
    tls: Tls | None = None


class StatUrls(_Closed):
    """The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business."""
    address: NonEmptyString


class StatEvents(_Closed):
    nats: Nats
    consumer: NatsConsumer


class Urls(ServiceConfig):
    """The service that owns the URL tables. Everything that writes them asks it."""
    model_config = ConfigDict(extra="forbid")
    listen: Listen
    tls: Tls | None = None


class Web(ServiceConfig):
    """The front end: serve the page, and ask the service that owns the tables. It writes nothing and holds no database credential."""
    model_config = ConfigDict(extra="forbid")
    listen: Listen
    tls: Tls | None = None
    #: The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business.
    urls: WebUrls
    #: The Content-Security-Policy this server sends with every response. Absent means report-only with no extra origins: the header is sent, nothing is blocked, so turning it on cannot break a page.
    csp: WebCsp | None = None
    #: Browser telemetry (Grafana Faro). Absent, or `enabled: false`, means the page sends nothing and the Faro libraries are never downloaded. Every field here is written into the page for every visitor to read: `apiKey` is a PUBLIC identifier of this app at the collector (one key per app, rotatable, paired with the collector's origin allow-list), not a secret, and no credential may be put in this block.
    faro: WebFaro | None = None
    #: Where the built page is. A path rather than an embedded bundle, because the assets are the one thing in this image that a CDN might serve instead.
    assets: WebAssets


class WebUrls(_Closed):
    """The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business."""
    address: NonEmptyString


class WebCsp(_Closed):
    """The Content-Security-Policy this server sends with every response. Absent means report-only with no extra origins: the header is sent, nothing is blocked, so turning it on cannot break a page."""
    #: `report-only` sends Content-Security-Policy-Report-Only: a browser reports a violation to its console (and to reportUri) and blocks nothing. `enforce` sends Content-Security-Policy. `off` sends neither.
    mode: CspMode = Field(default="report-only")
    #: Origins the page may connect to besides its own, added to `connect-src 'self'`. Each is a bare origin, scheme and host and optional port, never a path and never a keyword.
    connectSrc: list[HttpOrigin] = Field(default=[])
    #: Where a browser POSTs violation reports. A path on this origin or an absolute URL; empty or absent adds no report-uri directive.
    reportUri: ReportUri | None = None


class WebFaro(_Closed):
    """Browser telemetry (Grafana Faro). Absent, or `enabled: false`, means the page sends nothing and the Faro libraries are never downloaded. Every field here is written into the page for every visitor to read: `apiKey` is a PUBLIC identifier of this app at the collector (one key per app, rotatable, paired with the collector's origin allow-list), not a secret, and no credential may be put in this block."""
    #: Whether the page sends telemetry at all.
    enabled: bool = Field(default=False)
    #: Where the page POSTs telemetry: a path on its own origin (the default, `/faro/collect`, which the gateway routes to the collector, so connect-src 'self' is enough) or an absolute HTTPS URL, whose origin is then added to connect-src.
    collectorUrl: CollectorUrl = Field(default="/faro/collect")
    #: The app's public key at the collector, sent as `x-api-key`. A public identifier, not a secret.
    apiKey: PublicKey | None = None
    #: The app name the collector sees.
    appName: NonEmptyString = Field(default="url-shortener-web")
    #: A label for where this install runs, for example `devel`.
    environment: str | None = None
    #: The fraction of browser SESSIONS that report anything, 0 to 1.
    sampleRate: Ratio = Field(default=1)


class WebAssets(_Closed):
    """Where the built page is. A path rather than an embedded bundle, because the assets are the one thing in this image that a CDN might serve instead."""
    directory: NonEmptyString


class Tls(TlsFields):

    @model_validator(mode="after")
    def _conditional_Tls(self) -> "Tls":
        if _at(self, ["mode"]) in ["permissive", "strict"]:
            missing = [".".join(p) for p in [["certFile"], ["keyFile"], ["caFile"], ["trustDomain"]] if _at(self, p) is None]
            if missing:
                raise ValueError("when `mode` is `permissive` or `strict`, `certFile`, `keyFile`, `caFile`, `trustDomain` are required: missing " + ", ".join(missing))
        return self


SCHEMAS: dict[str, type[BaseModel]] = {
    "echo": Echo,
    "fragments/bucket": Bucket,
    "fragments/drain": Drain,
    "fragments/listen": Listen,
    "fragments/log": Log,
    "fragments/nats": Nats,
    "fragments/nats-consumer": NatsConsumer,
    "fragments/platform": Platform,
    "fragments/postgres": Postgres,
    "fragments/probes": Probes,
    "fragments/tls": Tls,
    "log": Archiver,
    "migrate": Migrate,
    "prober": Prober,
    "redirect": Redirect,
    "service": ServiceConfig,
    "stat": Stat,
    "urls": Urls,
    "web": Web,
}
