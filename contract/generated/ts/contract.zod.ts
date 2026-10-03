// Generated from a contract by contracts.typescript. Do not edit.
import { z } from "zod";

const noNewline = (s: string): boolean => !s.includes("\n");

/** host:port as the configuration contract spells it today. It does NOT bound the port at 65535, because the hand-written pattern does not; `Port` is the stricter vocabulary a contract may move to. */
export const HostPort = z.string().regex(/^[^\s]*:[0-9]{1,5}$/).refine(noNewline);
export type HostPort = z.infer<typeof HostPort>;
/** How a workload's transport is authenticated. */
export const TlsMode = z.enum(["off", "permissive", "strict"]);
export type TlsMode = z.infer<typeof TlsMode>;
/** A string of at least one character. */
export const NonEmptyString = z.string().min(1);
export type NonEmptyString = z.infer<typeof NonEmptyString>;
/** The name of an environment variable. */
export const EnvName = z.string().regex(/^[A-Za-z_][A-Za-z0-9_]*$/).refine(noNewline);
export type EnvName = z.infer<typeof EnvName>;
/** The lowest level a service writes. */
export const LogLevel = z.enum(["debug", "info", "warn", "error"]);
export type LogLevel = z.infer<typeof LogLevel>;
/** A whole number of at least one. */
export const PositiveInt = z.number().int().min(1);
export type PositiveInt = z.infer<typeof PositiveInt>;
/** An RFC 3986 URI. `format: uri` is an annotation here, never asserted, so the alias asserts nothing either. */
export const Url = z.string();
export type Url = z.infer<typeof Url>;
/** An image digest, or empty when there is none. */
export const ImageDigest = z.string().regex(/^(sha256:[0-9a-f]{64})?$/).refine(noNewline);
export type ImageDigest = z.infer<typeof ImageDigest>;
/** When a container image is pulled. */
export const PullPolicy = z.enum(["Always", "IfNotPresent", "Never"]);
export type PullPolicy = z.infer<typeof PullPolicy>;
/** A whole number of at least zero. */
export const NonNegativeInt = z.number().int().min(0);
export type NonNegativeInt = z.infer<typeof NonNegativeInt>;
/** Kubernetes' own object shape, passed through unchanged. */
export const OpenObject = z.looseObject({});
export type OpenObject = z.infer<typeof OpenObject>;
/** A path that starts at the root, the root itself included. */
export const RootedPath = z.string().regex(/^\//).refine(noNewline);
export type RootedPath = z.infer<typeof RootedPath>;
/** An absolute path that is not the root. */
export const AbsPath = z.string().regex(/^\/.+/).refine(noNewline);
export type AbsPath = z.infer<typeof AbsPath>;
/** The protocol OpenTelemetry exports over. */
export const OtelProtocol = z.enum(["grpc", "http/protobuf", "http/json"]);
export type OtelProtocol = z.infer<typeof OtelProtocol>;
/** An open object that must carry `name`. */
export const Named = z.looseObject({}).refine((o) => ["name"].every((k) => k in o));
export type Named = z.infer<typeof Named>;
/** An open object that must carry `name` and `mountPath`. */
export const Mounted = z.looseObject({}).refine((o) => ["name", "mountPath"].every((k) => k in o));
export type Mounted = z.infer<typeof Mounted>;
/** A PostgreSQL connection URL, by its scheme. */
export const PostgresUrl = z.string().regex(/^postgres(ql)?:\/\//).refine(noNewline);
export type PostgresUrl = z.infer<typeof PostgresUrl>;
/** A duration in Go's spelling, as a string: `10s`, `500ms`. Pkl's own `Duration` renders as an object, which no other language reads. */
export const GoDuration = z.string().regex(/^[0-9]+(ns|us|µs|ms|s|m|h)$/).refine(noNewline);
export type GoDuration = z.infer<typeof GoDuration>;
/** What a page's content security policy does. */
export const CspMode = z.enum(["off", "report-only", "enforce"]);
export type CspMode = z.infer<typeof CspMode>;
/** A bare web origin: scheme, host and an optional port, never a path. */
export const HttpOrigin = z.string().regex(/^https?:\/\/[A-Za-z0-9.*-]+(:[0-9]+)?$/).refine(noNewline);
export type HttpOrigin = z.infer<typeof HttpOrigin>;
/** Where a browser reports a violation: empty, a path or an absolute URL, with nothing that would end a directive. */
export const ReportUri = z.string().regex(/^[^\s;,'"]*$/).refine(noNewline);
export type ReportUri = z.infer<typeof ReportUri>;
/** Where a page posts telemetry: a path on its own origin, or an absolute HTTPS URL. */
export const CollectorUrl = z.string().regex(/^(https:\/\/[^\s'"<>;,]+|\/[^\s'"<>;,?#]*)$/).refine(noNewline);
export type CollectorUrl = z.infer<typeof CollectorUrl>;
/** A public identifier: URL-safe characters only. Not a secret. */
export const PublicKey = z.string().regex(/^[A-Za-z0-9._~-]*$/).refine(noNewline);
export type PublicKey = z.infer<typeof PublicKey>;
/** A fraction, zero to one. */
export const Ratio = z.number().min(0).max(1);
export type Ratio = z.infer<typeof Ratio>;

/** The envelope every service's configuration carries. A service extends this module and adds its own properties beside it:      extends "package://.../contracts.templates@<version>#/ServiceConfig.pkl"  What is in here is what EVERY component has, including a job that exits and a consumer that answers nothing: somewhere to report health, a log level, and a shutdown budget. A listener is not one of those, so it is not here.  The module is open so that a service can extend it, and its classes are closed, so a key that is not declared is refused: a typo must fail. */
export const ServiceConfig_shape = {
  /** The health listener. Required: every component can be probed. */
  probes: z.lazy(() => Probes),
  /** Structured logging. Absent means the service's own default level. */
  log: z.lazy(() => Log).optional(),
  /** The shutdown budget. Absent means the service's own default, which is only correct if nothing external is counting. */
  drain: z.lazy(() => Drain).optional(),
};
export const ServiceConfig = z.looseObject(ServiceConfig_shape);
export type ServiceConfig = z.infer<typeof ServiceConfig>;

/** A service that answers with a greeting.  Not a url-shortener component: it is the service of the chart in `charts/testdata/service-example`, the smallest chart that follows the library convention (docs/guides/charts.md), and is modelled here because that chart's values are exactly the two blocks `ChartValues` is made of. */
export const Echo_shape = {
  ...ServiceConfig_shape,
  listen: z.lazy(() => Listen),
  tls: z.lazy(() => Tls).optional(),
  greeting: NonEmptyString,
  /** A store the service reads. The token is a SECRET, so the file names the environment variable that holds it and the chart declares that variable in `platform.secrets`. */
  store: z.lazy(() => EchoStore).optional(),
};
export const Echo = z.strictObject(Echo_shape);
export type Echo = z.infer<typeof Echo>;

/** A TCP listener. `address` is a host:port the service binds; an empty host binds every interface. */
export const Listen_shape = {
  /** host:port, for example ":8080" or "127.0.0.1:8080". */
  address: HostPort,
};
export const Listen = z.strictObject(Listen_shape);
export type Listen = z.infer<typeof Listen>;

/** Mutually authenticated transport, where the platform provides the identity. A workload presents a certificate it did not mint, reloads it without restarting, and admits peers by the account they run as rather than by the address they call from. Absent, or mode 'off', means cleartext: a service must be installable on a platform that provides none of this. */
export const TlsFields_shape = {
  /** 'off' serves cleartext only. 'permissive' serves BOTH, on two ports, so that an edge can migrate one side at a time without a coordinated window. 'strict' serves only the authenticated port. One listener cannot be both in every runtime, which is why permissive is two ports rather than one that sniffs. */
  mode: TlsMode.default("off"),
  /** Where the authenticated listener binds under 'permissive', beside the cleartext one. Under 'strict' there is one listener and it is the service's own, so this is unused: the protocol changes, the address does not, and nothing downstream has to be told. */
  address: NonEmptyString.optional(),
  /** The certificate this workload presents, mounted and rotated by the platform. Re-read when it changes, never cached for the process's lifetime: a one-hour certificate outlives no deployment. */
  certFile: NonEmptyString.optional(),
  /** Its private key. It lives in the pod and never in a secret, so a workload that can read secrets in its namespace still cannot read a neighbour's key. */
  keyFile: NonEmptyString.optional(),
  /** The authority peers are verified against, distributed by the platform as a trust bundle. */
  caFile: NonEmptyString.optional(),
  /** The root of every identity this service will admit, for example 'example.internal'. A peer whose identity belongs to another trust domain is refused before its account is even considered. */
  trustDomain: NonEmptyString.optional(),
  /** Who may call. Each entry is an ACCOUNT, not an address: an address resolves to whoever holds it today. An empty list admits no one, which is the correct default for a service nobody has been granted. */
  peers: z.array(z.lazy(() => TlsPeer)).optional(),
};
export const TlsFields = z.strictObject(TlsFields_shape);
export type TlsFields = z.infer<typeof TlsFields>;

/** One account that may call: the namespace it runs in and its ServiceAccount name. */
export const TlsPeer_shape = {
  /** The namespace the account lives in. */
  namespace: NonEmptyString,
  /** The ServiceAccount the peer runs as. */
  serviceAccount: NonEmptyString,
};
export const TlsPeer = z.strictObject(TlsPeer_shape);
export type TlsPeer = z.infer<typeof TlsPeer>;

/** A store the service reads. The token is a SECRET, so the file names the environment variable that holds it and the chart declares that variable in `platform.secrets`. */
export const EchoStore_shape = {
  tokenEnv: EnvName,
};
export const EchoStore = z.strictObject(EchoStore_shape);
export type EchoStore = z.infer<typeof EchoStore>;

/** The health listener. Separate from the service's own traffic, so that readiness is answerable when the service's listener is saturated, and so that a probe is not reachable from outside. */
export const Probes_shape = {
  /** host:port for /health/live and /health/ready. */
  address: HostPort,
};
export const Probes = z.strictObject(Probes_shape);
export type Probes = z.infer<typeof Probes>;

/** Structured logging. One level for the whole service: per-package levels are deliberately not part of the contract. */
export const Log_shape = {
  /** The lowest level that is written. */
  level: LogLevel.default("info"),
};
export const Log = z.strictObject(Log_shape);
export type Log = z.infer<typeof Log>;

/** How long the service may take to finish in-flight work after SIGTERM. It is one number shared with whatever deploys the service: the grace period granted to the process and the pre-stop delay before it are derived from this, so that a draining process is never killed at the moment it would have finished. */
export const Drain_shape = {
  /** Seconds to finish in-flight work. Unset means the service's own default, which is only correct if nothing external is counting. */
  seconds: PositiveInt.default(20),
};
export const Drain = z.strictObject(Drain_shape);
export type Drain = z.infer<typeof Drain>;

/** An object store addressed by the S3 API. The vendor is not part of the configuration: an endpoint, a region and a path-style flag are enough to reach a cloud service, an in-cluster store or a test double, and a service that hard-codes one of them cannot be tested without it. */
export const Bucket_shape = {
  /** The bucket. It exists already: a service does not create its own store. */
  name: NonEmptyString,
  /** The region the bucket is in. */
  region: NonEmptyString.optional(),
  /** Override the API endpoint. Unset means the SDK's own resolution for the region. */
  endpoint: Url.optional(),
  /** Path to a certificate authority bundle the platform mounts, for a store whose endpoint is not signed by a public root. A store inside somebody's own network is the ordinary case, not the exotic one. Unset means the system trust store. */
  ca: NonEmptyString.optional(),
  /** Address the bucket as a path rather than as a host. Required by most non-cloud implementations. */
  pathStyle: z.boolean().default(false),
  /** The NAMES of the environment variables holding the credentials, never the values. Unset means the SDK's ambient credentials, which is what a workload identity provides. */
  credentialsEnv: z.lazy(() => BucketCredentialsEnv).optional(),
};
export const Bucket = z.strictObject(Bucket_shape);
export type Bucket = z.infer<typeof Bucket>;

/** The NAMES of the environment variables holding the credentials, never the values. Unset means the SDK's ambient credentials, which is what a workload identity provides. */
export const BucketCredentialsEnv_shape = {
  /** The name of the variable holding the access key ID. */
  accessKeyID: NonEmptyString,
  /** The name of the variable holding the secret access key. */
  secretAccessKey: NonEmptyString,
};
export const BucketCredentialsEnv = z.strictObject(BucketCredentialsEnv_shape);
export type BucketCredentialsEnv = z.infer<typeof BucketCredentialsEnv>;

/** A NATS connection, and nothing else. What a service does with the connection — publish to a subject, bind a durable consumer to a stream — differs per component and is described beside it: a publisher with a `consumer` field it never reads is a field somebody will eventually set. */
export const Nats_shape = {
  /** The server URL, for example nats://nats:4222. */
  url: NonEmptyString,
  /** Path to a file holding the token the client authenticates with, mounted by the platform and re-read on every reconnect so that a rotated token is picked up without a restart. It is the workload's own account token: the broker asks an authorisation service who the bearer is, and that service answers from the account rather than from anything the client claims. Unset with `tls` unset means no authentication, which is a test configuration and not a deployment; unset with `tls` set means the certificate is the credential. */
  tokenFile: NonEmptyString.optional(),
  /** Connect over TLS and authenticate with the service's own workload identity instead of a token. The certificate presented is the one the service's own top-level `tls` block loads (its mode must not be `off`); the broker maps the identity in it to a user with its own permissions, so what the service may do is the broker's decision and nothing in this file. The certificate is re-read on every connect, so the platform's rotation is picked up without a restart and a rotation drops no connection already made. Only a service that declares this key in its own schema acts on it. */
  tls: z.lazy(() => NatsTls).optional(),
};
export const Nats = z.strictObject(Nats_shape);
export type Nats = z.infer<typeof Nats>;

/** Connect over TLS and authenticate with the service's own workload identity instead of a token. The certificate presented is the one the service's own top-level `tls` block loads (its mode must not be `off`); the broker maps the identity in it to a user with its own permissions, so what the service may do is the broker's decision and nothing in this file. The certificate is re-read on every connect, so the platform's rotation is picked up without a restart and a rotation drops no connection already made. Only a service that declares this key in its own schema acts on it. */
export const NatsTls_shape = {
  /** Path to the trust bundle the BROKER's certificate is verified against. The broker's, not the workload identity's: a broker has a name and no workload identity, and usually a different chain. */
  caFile: NonEmptyString,
  /** The name the broker's certificate was issued for, when that is not the host in `url`. Empty verifies the host dialled. */
  serverName: z.string().optional(),
};
export const NatsTls = z.strictObject(NatsTls_shape);
export type NatsTls = z.infer<typeof NatsTls>;

/** What a consumer binds to: a stream, a durable name, and the subject it filters. Durable by name, because a consumer that forgets its position on restart replays or loses whatever arrived while it was gone. */
export const NatsConsumer_shape = {
  /** The stream to consume from. It exists already: a service does not create the stream it reads. */
  stream: NonEmptyString,
  /** The durable consumer name. Shared by every replica of this component, which is what makes them one consumer group rather than several. */
  durable: NonEmptyString,
  /** Filter the stream to this subject. Unset consumes everything the stream holds. */
  subject: NonEmptyString.optional(),
};
export const NatsConsumer = z.strictObject(NatsConsumer_shape);
export type NatsConsumer = z.infer<typeof NatsConsumer>;

/** What the platform provides one component of a service chart: the image, the replicas, the account it runs as, how it is probed, where its identity is mounted, which secrets reach it as environment variables, and how it exports telemetry. The shape of the `platform` block of a chart's values, read by the library chart (decision 0009 of the policy repository). Nothing here is the service's own configuration: that is the chart's `config` block, which is the service's schema and nothing else. */
export const Platform_shape = {
  /** Where the image is. A digest when there is one; a tag only when there is not. Left out, the library reads the chart's own top-level `images.<component>`, the map a release stamps and refuses to publish with an empty digest. */
  image: z.lazy(() => PlatformImage).optional(),
  /** Defaults to IfNotPresent. */
  imagePullPolicy: PullPolicy.optional(),
  /** Defaults to 1. A service that must survive a rollout runs more than one: one instance cannot be replaced without a gap whatever the strategy says. */
  replicas: NonNegativeInt.optional(),
  /** The rolling update. The defaults make a rollout gapless: the replacement is READY before the incumbent is touched. */
  strategy: z.lazy(() => PlatformStrategy).optional(),
  /** Kubernetes' own resource requirements. Open: it is passed through unchanged. */
  resources: OpenObject.optional(),
  /** Who the process is. Every field defaults to 65532, the unprivileged user the runtime images run as; `fsGroup` is the one that matters, because a CSI driver writes what it mounts owned by root. */
  podSecurity: z.lazy(() => PlatformPodSecurity).optional(),
  /** The account this component runs as. EVERY component has its own, always; `default` is refused. The library grants nothing: annotations are where a platform binds the account to rights outside the cluster, and which mechanism does that is the platform's. */
  serviceAccount: z.lazy(() => PlatformServiceAccount).optional(),
  /** A component that listens has a Service on the ports of its own `config.listen`; one that does not has none. */
  service: z.lazy(() => PlatformService).optional(),
  /** How the component is probed, on the listener its own `config.probes` binds. Liveness is nothing but the process: a probe that checks a dependency restarts a healthy process and makes an outage worse. */
  probes: z.lazy(() => PlatformProbes).optional(),
  /** The service's own shutdown budget is `config.drain.seconds`; the library derives the grace period from it and this delay, so the three numbers cannot disagree. */
  drain: z.lazy(() => PlatformDrain).optional(),
  /** Where the platform mounts the workload identity. The files the component's own `config.tls` names must be under `mountPath`; the render refuses a file that is not. */
  tls: z.lazy(() => PlatformTls).optional(),
  /** OpenTelemetry's own environment variables (decision 0006 of the policy repository): they leave the chart as variables, never as configuration keys. No endpoint means do not export. */
  telemetry: z.lazy(() => PlatformTelemetry).optional(),
  /** The environment variables that carry SECRETS, and nothing else (decision 0002 of the policy repository): variable name to the Secret and key its value comes from. The configuration file names the VARIABLE; the value never appears in a values file or a render. */
  secrets: z.record(EnvName, z.lazy(() => PlatformSecret)).optional(),
  /** Environment a platform CLIENT LIBRARY reads (a database client's connection variables, for example), never the service's own configuration: a service takes no other structural input than its file (decision 0002 of the policy repository). A secret does not belong here; declare it in `secrets`. */
  env: z.array(z.lazy(() => PlatformEnvVar)).optional(),
  /** Extra pod volumes, in Kubernetes' own shape, for what a client library mounts (a trust bundle, a password file). Open: passed through unchanged. */
  volumes: z.array(Named).optional(),
  /** The mounts for `volumes`, in Kubernetes' own shape. */
  volumeMounts: z.array(Mounted).optional(),
  /** How the file reaches the process. The path is ONE argument or ONE environment variable, never both (decision 0002 of the policy repository). */
  config: z.lazy(() => PlatformConfig).optional(),
  /** The ConfigMap the file is rendered into. */
  configMap: z.lazy(() => PlatformConfigMap).optional(),
};
export const Platform = z.strictObject(Platform_shape);
export type Platform = z.infer<typeof Platform>;

/** Where the image is. A digest when there is one; a tag only when there is not. Left out, the library reads the chart's own top-level `images.<component>`, the map a release stamps and refuses to publish with an empty digest. */
export const PlatformImage_shape = {
  /** The registry host. Left out, the repository is read as the whole name. */
  registry: z.string().optional(),
  /** The repository path, without the registry and without a tag. */
  repository: NonEmptyString,
  /** The tag. Empty or absent when there is a digest. */
  tag: z.string().optional(),
  /** The content digest, `sha256:` and 64 hex digits; empty when there is none. */
  digest: ImageDigest.optional(),
};
export const PlatformImage = z.strictObject(PlatformImage_shape);
export type PlatformImage = z.infer<typeof PlatformImage>;

/** The rolling update. The defaults make a rollout gapless: the replacement is READY before the incumbent is touched. */
export const PlatformStrategy_shape = {
  /** Defaults to 0. */
  maxUnavailable: z.union([z.number().int(), z.string()]).optional(),
  /** Defaults to 1. */
  maxSurge: z.union([z.number().int(), z.string()]).optional(),
};
export const PlatformStrategy = z.strictObject(PlatformStrategy_shape);
export type PlatformStrategy = z.infer<typeof PlatformStrategy>;

/** Who the process is. Every field defaults to 65532, the unprivileged user the runtime images run as; `fsGroup` is the one that matters, because a CSI driver writes what it mounts owned by root. */
export const PlatformPodSecurity_shape = {
  /** The user ID the process runs as. Never zero. */
  runAsUser: PositiveInt.optional(),
  /** The group ID the process runs as. Never zero. */
  runAsGroup: PositiveInt.optional(),
  /** The group that owns what a CSI driver mounts. Never zero. */
  fsGroup: PositiveInt.optional(),
};
export const PlatformPodSecurity = z.strictObject(PlatformPodSecurity_shape);
export type PlatformPodSecurity = z.infer<typeof PlatformPodSecurity>;

/** The account this component runs as. EVERY component has its own, always; `default` is refused. The library grants nothing: annotations are where a platform binds the account to rights outside the cluster, and which mechanism does that is the platform's. */
export const PlatformServiceAccount_shape = {
  /** False where the platform creates the accounts; they must then exist. Defaults to true. */
  create: z.boolean().optional(),
  /** Defaults to `<release>-<component>`. */
  name: NonEmptyString.optional(),
  /** Annotations put on the account, where a platform binds it to rights outside the cluster. */
  annotations: z.record(z.string(), z.string()).optional(),
};
export const PlatformServiceAccount = z.strictObject(PlatformServiceAccount_shape);
export type PlatformServiceAccount = z.infer<typeof PlatformServiceAccount>;

/** A component that listens has a Service on the ports of its own `config.listen`; one that does not has none. */
export const PlatformService_shape = {
  /** Defaults to whether `config.listen` exists. Enabling one for a component that listens on nothing is refused. */
  enabled: z.boolean().optional(),
};
export const PlatformService = z.strictObject(PlatformService_shape);
export type PlatformService = z.infer<typeof PlatformService>;

/** How the component is probed, on the listener its own `config.probes` binds. Liveness is nothing but the process: a probe that checks a dependency restarts a healthy process and makes an outage worse. */
export const PlatformProbes_shape = {
  /** The liveness probe: the process and nothing else. */
  liveness: z.lazy(() => PlatformProbe).optional(),
  /** The readiness probe: this instance can serve now. */
  readiness: z.lazy(() => PlatformProbe).optional(),
  /** Absent means no startup probe. Present, it needs at least one field. */
  startup: z.lazy(() => PlatformProbe).optional(),
};
export const PlatformProbes = z.strictObject(PlatformProbes_shape);
export type PlatformProbes = z.infer<typeof PlatformProbes>;

/** One probe's timing. Every field has the library's own default. */
export const PlatformProbe_shape = {
  /** The path on the probe listener. Defaults to the contract's own. */
  path: RootedPath.optional(),
  /** How often to probe, in seconds. */
  periodSeconds: PositiveInt.optional(),
  /** How long to wait after the start before the first probe, in seconds. */
  initialDelaySeconds: NonNegativeInt.optional(),
  /** How long one probe may take, in seconds. */
  timeoutSeconds: PositiveInt.optional(),
  /** Consecutive successes that make the probe pass again. */
  successThreshold: PositiveInt.optional(),
  /** Consecutive failures that make the probe fail. */
  failureThreshold: PositiveInt.optional(),
};
export const PlatformProbe = z.strictObject(PlatformProbe_shape);
export type PlatformProbe = z.infer<typeof PlatformProbe>;

/** The service's own shutdown budget is `config.drain.seconds`; the library derives the grace period from it and this delay, so the three numbers cannot disagree. */
export const PlatformDrain_shape = {
  /** Fail readiness, then wait this long before the drain starts, so that whatever routes traffic has removed this endpoint first. Defaults to 5. */
  preStopSeconds: NonNegativeInt.optional(),
};
export const PlatformDrain = z.strictObject(PlatformDrain_shape);
export type PlatformDrain = z.infer<typeof PlatformDrain>;

/** Where the platform mounts the workload identity. The files the component's own `config.tls` names must be under `mountPath`; the render refuses a file that is not. */
export const PlatformTls_shape = {
  /** The driver that mounts the identity. The platform's, so there is no default; required once the identity is mounted. */
  csiDriver: NonEmptyString.optional(),
  /** Defaults to /var/run/identity. */
  mountPath: AbsPath.optional(),
  /** Defaults to whether `config.tls.mode` is permissive or strict. True mounts the identity into a component that presents none of its own, because the release does; false never mounts it. */
  mount: z.boolean().optional(),
};
export const PlatformTls = z.strictObject(PlatformTls_shape);
export type PlatformTls = z.infer<typeof PlatformTls>;

/** OpenTelemetry's own environment variables (decision 0006 of the policy repository): they leave the chart as variables, never as configuration keys. No endpoint means do not export. */
export const PlatformTelemetry_shape = {
  /** Defaults to `<release>-<component>`. */
  serviceName: NonEmptyString.optional(),
  /** The collector to export to. Absent means do not export. */
  endpoint: z.string().optional(),
  /** The protocol to export over. */
  protocol: OtelProtocol.optional(),
  /** OpenTelemetry's `OTEL_TRACES_SAMPLER`. */
  tracesSampler: z.string().optional(),
  /** The sampler's argument: a ratio, as a number or a string. */
  sampleRatio: z.union([z.string(), z.number()]).optional(),
  /** Attributes that describe the resource, name to value. */
  resourceAttributes: z.record(z.string(), z.string()).optional(),
};
export const PlatformTelemetry = z.strictObject(PlatformTelemetry_shape);
export type PlatformTelemetry = z.infer<typeof PlatformTelemetry>;

/** Where an environment variable's value comes from: a key of a Secret. Never the value. */
export const PlatformSecret_shape = {
  /** The name of the Secret. */
  secretName: NonEmptyString,
  /** The key within it. */
  key: NonEmptyString,
};
export const PlatformSecret = z.strictObject(PlatformSecret_shape);
export type PlatformSecret = z.infer<typeof PlatformSecret>;

/** A plain environment variable. Never a secret. */
export const PlatformEnvVar_shape = {
  /** The variable's name. */
  name: NonEmptyString,
  /** Its value. */
  value: z.string(),
};
export const PlatformEnvVar = z.strictObject(PlatformEnvVar_shape);
export type PlatformEnvVar = z.infer<typeof PlatformEnvVar>;

/** How the file reaches the process. The path is ONE argument or ONE environment variable, never both (decision 0002 of the policy repository). */
export const PlatformConfig_shape = {
  /** Defaults to `<component>.yaml`. */
  fileName: NonEmptyString.optional(),
  /** The directory the ConfigMap is mounted at. Defaults to `/etc/<chart name>`. */
  mountPath: AbsPath.optional(),
  /** The argument that carries the path. Defaults to `-config`. */
  pathFlag: NonEmptyString.optional(),
  /** When set, the path is passed in this environment variable instead of an argument. */
  pathEnv: NonEmptyString.optional(),
};
export const PlatformConfig = z.strictObject(PlatformConfig_shape);
export type PlatformConfig = z.infer<typeof PlatformConfig>;

/** The ConfigMap the file is rendered into. */
export const PlatformConfigMap_shape = {
  /** For a ConfigMap that must be a hook resource: a pre-install job cannot mount one the release has not created yet. */
  annotations: z.record(z.string(), z.string()).optional(),
};
export const PlatformConfigMap = z.strictObject(PlatformConfigMap_shape);
export type PlatformConfigMap = z.infer<typeof PlatformConfigMap>;

/** A PostgreSQL connection. The URL carries no password: it names the environment variable that does. */
export const Postgres_shape = {
  /** A connection URL without credentials, for example postgres://user@host:5432/dbname?sslmode=require. */
  url: PostgresUrl,
  /** The NAME of the environment variable holding the password. Unset means the connection needs none. */
  passwordEnv: NonEmptyString.optional(),
  /** Pool size for this instance. Sized against the server's limit divided by the number of instances, not guessed. */
  maxConnections: PositiveInt.default(10),
};
export const Postgres = z.strictObject(Postgres_shape);
export type Postgres = z.infer<typeof Postgres>;

/** The archiver: consume what the redirect service recorded about each request, and write it to an object store as NDJSON. It owns no database and answers no calls. */
export const Archiver_shape = {
  ...ServiceConfig_shape,
  events: z.lazy(() => ArchiverEvents),
  archive: z.lazy(() => ArchiverArchive),
};
export const Archiver = z.strictObject(Archiver_shape);
export type Archiver = z.infer<typeof Archiver>;

export const ArchiverEvents_shape = {
  nats: z.lazy(() => Nats),
  consumer: z.lazy(() => NatsConsumer),
};
export const ArchiverEvents = z.strictObject(ArchiverEvents_shape);
export type ArchiverEvents = z.infer<typeof ArchiverEvents>;

export const ArchiverArchive_shape = {
  bucket: z.lazy(() => Bucket),
  /** What every key this component writes begins with. A bucket is usually shared, and a component that writes to the root of one cannot be given permission to write only its own objects. */
  prefix: z.string().default("url-shortener/requests"),
  /** When to close an object and write it. Both limits apply: whichever is reached first. A size-only rule means a quiet hour is never archived; a time-only rule means a busy one is archived in objects too small to be worth reading. */
  batch: z.lazy(() => ArchiverBatch).optional(),
};
export const ArchiverArchive = z.strictObject(ArchiverArchive_shape);
export type ArchiverArchive = z.infer<typeof ArchiverArchive>;

/** When to close an object and write it. Both limits apply: whichever is reached first. A size-only rule means a quiet hour is never archived; a time-only rule means a busy one is archived in objects too small to be worth reading. */
export const ArchiverBatch_shape = {
  /** Write once this many records are held. */
  maxRecords: PositiveInt.default(500),
  /** Write this long after the first record of a batch arrived, however few there are. */
  maxSeconds: PositiveInt.default(60),
};
export const ArchiverBatch = z.strictObject(ArchiverBatch_shape);
export type ArchiverBatch = z.infer<typeof ArchiverBatch>;

/** The migration job. It does NOT reference the service envelope: a job that runs once and exits is not a service, and probes it never serves would be configuration a deployment can set and watch do nothing. */
export const Migrate_shape = {
  log: z.lazy(() => Log).optional(),
  /** The role the migration becomes before it creates anything, so that tables are owned by the owner rather than by whoever migrated. Removing the migration user must not orphan the schema. */
  ownerRole: NonEmptyString,
  /** The role the running components use, granted on the app schemas after the migration lands. Unset skips the grant, which is correct where the platform grants it instead. */
  appRole: NonEmptyString.optional(),
};
export const Migrate = z.strictObject(Migrate_shape);
export type Migrate = z.infer<typeof Migrate>;

/** Always-on synthetic traffic: walk the SAME journeys a real caller does (create a short link, resolve it, watch its counter move), in a loop, over the two Services this release already serves. Separate from the e2e suite: the suite proves a release IS healthy once; this proves it STAYS healthy, so a bake window has signal to read even when nothing real is happening. */
export const Prober_shape = {
  ...ServiceConfig_shape,
  /** How often the loop repeats one full pass of every journey, as a Go duration string, for example "10s". */
  interval: GoDuration,
  /** Marks every long URL and key this prober invents, the same role examples/url-shortener/e2e/suite's own testDataPrefix plays for the e2e suite: a person reading the urls table or the archive bucket by hand can tell synthetic traffic from a real caller's at a glance. */
  keyPrefix: NonEmptyString.optional(),
  /** How long the "stat" journey keeps watching a link's click count AFTER it first reads exactly 1, as a Go duration string, and fails if it moves again. A consumer that fails to acknowledge a message has it redelivered after its ack wait, so a click counted twice looks right at the first read and wrong one ack wait later; this window has to outlast at least one redelivery, so it defaults to twice the consumer's 30s ack wait ("60s"). "0s" turns the hold off. */
  statSettle: GoDuration.optional(),
  /** The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is a platform decision this component does not make for itself. */
  urls: z.lazy(() => ProberUrls),
  /** The service that resolves a short key. An address and nothing else, on the same terms as `urls` above. */
  redirect: z.lazy(() => ProberRedirect),
  tls: z.lazy(() => Tls).optional(),
};
export const Prober = z.strictObject(Prober_shape);
export type Prober = z.infer<typeof Prober>;

/** The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is a platform decision this component does not make for itself. */
export const ProberUrls_shape = {
  address: NonEmptyString,
};
export const ProberUrls = z.strictObject(ProberUrls_shape);
export type ProberUrls = z.infer<typeof ProberUrls>;

/** The service that resolves a short key. An address and nothing else, on the same terms as `urls` above. */
export const ProberRedirect_shape = {
  address: NonEmptyString,
};
export const ProberRedirect = z.strictObject(ProberRedirect_shape);
export type ProberRedirect = z.infer<typeof ProberRedirect>;

/** The redirect service: resolve a short key and emit what happened. */
export const Redirect_shape = {
  ...ServiceConfig_shape,
  listen: z.lazy(() => Listen),
  tls: z.lazy(() => Tls).optional(),
  events: z.lazy(() => RedirectEvents),
};
export const Redirect = z.strictObject(Redirect_shape);
export type Redirect = z.infer<typeof Redirect>;

export const RedirectEvents_shape = {
  nats: z.lazy(() => Nats),
  /** Where a resolved redirect is published. One event kind per subject, so a consumer never has to guess what it decoded. */
  redirectSubject: NonEmptyString,
  /** Where the request log is published. A separate subject from the redirect, for the same reason. */
  requestSubject: NonEmptyString,
};
export const RedirectEvents = z.strictObject(RedirectEvents_shape);
export type RedirectEvents = z.infer<typeof RedirectEvents>;

/** The click counter: consume redirects, and ask the service that owns the table to count them. */
export const Stat_shape = {
  ...ServiceConfig_shape,
  /** The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business. */
  urls: z.lazy(() => StatUrls),
  events: z.lazy(() => StatEvents),
  tls: z.lazy(() => Tls).optional(),
};
export const Stat = z.strictObject(Stat_shape);
export type Stat = z.infer<typeof Stat>;

/** The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business. */
export const StatUrls_shape = {
  address: NonEmptyString,
};
export const StatUrls = z.strictObject(StatUrls_shape);
export type StatUrls = z.infer<typeof StatUrls>;

export const StatEvents_shape = {
  nats: z.lazy(() => Nats),
  consumer: z.lazy(() => NatsConsumer),
};
export const StatEvents = z.strictObject(StatEvents_shape);
export type StatEvents = z.infer<typeof StatEvents>;

/** The service that owns the URL tables. Everything that writes them asks it. */
export const Urls_shape = {
  ...ServiceConfig_shape,
  listen: z.lazy(() => Listen),
  tls: z.lazy(() => Tls).optional(),
};
export const Urls = z.strictObject(Urls_shape);
export type Urls = z.infer<typeof Urls>;

/** The front end: serve the page, and ask the service that owns the tables. It writes nothing and holds no database credential. */
export const Web_shape = {
  ...ServiceConfig_shape,
  listen: z.lazy(() => Listen),
  tls: z.lazy(() => Tls).optional(),
  /** The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business. */
  urls: z.lazy(() => WebUrls),
  /** The Content-Security-Policy this server sends with every response. Absent means report-only with no extra origins: the header is sent, nothing is blocked, so turning it on cannot break a page. */
  csp: z.lazy(() => WebCsp).optional(),
  /** Browser telemetry (Grafana Faro). Absent, or `enabled: false`, means the page sends nothing and the Faro libraries are never downloaded. Every field here is written into the page for every visitor to read: `apiKey` is a PUBLIC identifier of this app at the collector (one key per app, rotatable, paired with the collector's origin allow-list), not a secret, and no credential may be put in this block. */
  faro: z.lazy(() => WebFaro).optional(),
  /** Where the built page is. A path rather than an embedded bundle, because the assets are the one thing in this image that a CDN might serve instead. */
  assets: z.lazy(() => WebAssets),
};
export const Web = z.strictObject(Web_shape);
export type Web = z.infer<typeof Web>;

/** The service that owns the URL tables. An address and nothing else: which protocol the caller speaks is in its code, and whether the connection is authenticated is the tls block's business. */
export const WebUrls_shape = {
  address: NonEmptyString,
};
export const WebUrls = z.strictObject(WebUrls_shape);
export type WebUrls = z.infer<typeof WebUrls>;

/** The Content-Security-Policy this server sends with every response. Absent means report-only with no extra origins: the header is sent, nothing is blocked, so turning it on cannot break a page. */
export const WebCsp_shape = {
  /** `report-only` sends Content-Security-Policy-Report-Only: a browser reports a violation to its console (and to reportUri) and blocks nothing. `enforce` sends Content-Security-Policy. `off` sends neither. */
  mode: CspMode.default("report-only"),
  /** Origins the page may connect to besides its own, added to `connect-src 'self'`. Each is a bare origin, scheme and host and optional port, never a path and never a keyword. */
  connectSrc: z.array(HttpOrigin).default([]),
  /** Where a browser POSTs violation reports. A path on this origin or an absolute URL; empty or absent adds no report-uri directive. */
  reportUri: ReportUri.optional(),
};
export const WebCsp = z.strictObject(WebCsp_shape);
export type WebCsp = z.infer<typeof WebCsp>;

/** Browser telemetry (Grafana Faro). Absent, or `enabled: false`, means the page sends nothing and the Faro libraries are never downloaded. Every field here is written into the page for every visitor to read: `apiKey` is a PUBLIC identifier of this app at the collector (one key per app, rotatable, paired with the collector's origin allow-list), not a secret, and no credential may be put in this block. */
export const WebFaro_shape = {
  /** Whether the page sends telemetry at all. */
  enabled: z.boolean().default(false),
  /** Where the page POSTs telemetry: a path on its own origin (the default, `/faro/collect`, which the gateway routes to the collector, so connect-src 'self' is enough) or an absolute HTTPS URL, whose origin is then added to connect-src. */
  collectorUrl: CollectorUrl.default("/faro/collect"),
  /** The app's public key at the collector, sent as `x-api-key`. A public identifier, not a secret. */
  apiKey: PublicKey.optional(),
  /** The app name the collector sees. */
  appName: NonEmptyString.default("url-shortener-web"),
  /** A label for where this install runs, for example `devel`. */
  environment: z.string().optional(),
  /** The fraction of browser SESSIONS that report anything, 0 to 1. */
  sampleRate: Ratio.default(1),
};
export const WebFaro = z.strictObject(WebFaro_shape);
export type WebFaro = z.infer<typeof WebFaro>;

/** Where the built page is. A path rather than an embedded bundle, because the assets are the one thing in this image that a CDN might serve instead. */
export const WebAssets_shape = {
  directory: NonEmptyString,
};
export const WebAssets = z.strictObject(WebAssets_shape);
export type WebAssets = z.infer<typeof WebAssets>;

/** Mutually authenticated transport, where the platform provides the identity. A workload presents a certificate it did not mint, reloads it without restarting, and admits peers by the account they run as rather than by the address they call from. Absent, or mode 'off', means cleartext: a service must be installable on a platform that provides none of this. */
export const Tls = TlsFields.refine((o) => !["permissive", "strict"].includes(o.mode) || ["certFile", "keyFile", "caFile", "trustDomain"].every((k) => (o as Record<string, unknown>)[k] !== undefined), { message: "certFile, keyFile, caFile, trustDomain required when mode is permissive or strict" });
export type Tls = z.infer<typeof Tls>;

export const schemas = {
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
} as const;
