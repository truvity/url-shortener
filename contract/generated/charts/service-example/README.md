<!-- Generated from the chart's values contract. Do not edit. -->

## Values

| Key | Type | Default | Description |
|---|---|---|---|
| `service-lib.global` | OpenObject |  | Helm's own `global` block, passed through unchanged. |
| `platform.image.registry` | string |  | The registry host. Left out, the repository is read as the whole name. |
| `platform.image.repository` | NonEmptyString |  | The repository path, without the registry and without a tag. |
| `platform.image.tag` | string |  | The tag. Empty or absent when there is a digest. |
| `platform.image.digest` | ImageDigest |  | The content digest, `sha256:` and 64 hex digits; empty when there is none. |
| `platform.imagePullPolicy` | PullPolicy |  | Defaults to IfNotPresent. |
| `platform.replicas` | NonNegativeInt |  | Defaults to 1. A service that must survive a rollout runs more than one: one instance cannot be replaced without a gap whatever the strategy says. |
| `platform.strategy.maxUnavailable` | integer \| string |  | Defaults to 0. |
| `platform.strategy.maxSurge` | integer \| string |  | Defaults to 1. |
| `platform.resources` | OpenObject |  | Kubernetes' own resource requirements. Open: it is passed through unchanged. |
| `platform.podSecurity.runAsUser` | PositiveInt |  | The user ID the process runs as. Never zero. |
| `platform.podSecurity.runAsGroup` | PositiveInt |  | The group ID the process runs as. Never zero. |
| `platform.podSecurity.fsGroup` | PositiveInt |  | The group that owns what a CSI driver mounts. Never zero. |
| `platform.serviceAccount.create` | boolean |  | False where the platform creates the accounts; they must then exist. Defaults to true. |
| `platform.serviceAccount.name` | NonEmptyString |  | Defaults to `<release>-<component>`. |
| `platform.serviceAccount.annotations` | map of string |  | Annotations put on the account, where a platform binds it to rights outside the cluster. |
| `platform.service.enabled` | boolean |  | Defaults to whether `config.listen` exists. Enabling one for a component that listens on nothing is refused. |
| `platform.probes.liveness.path` | RootedPath |  | The path on the probe listener. Defaults to the contract's own. |
| `platform.probes.liveness.periodSeconds` | PositiveInt |  | How often to probe, in seconds. |
| `platform.probes.liveness.initialDelaySeconds` | NonNegativeInt |  | How long to wait after the start before the first probe, in seconds. |
| `platform.probes.liveness.timeoutSeconds` | PositiveInt |  | How long one probe may take, in seconds. |
| `platform.probes.liveness.successThreshold` | PositiveInt |  | Consecutive successes that make the probe pass again. |
| `platform.probes.liveness.failureThreshold` | PositiveInt |  | Consecutive failures that make the probe fail. |
| `platform.probes.readiness.path` | RootedPath |  | The path on the probe listener. Defaults to the contract's own. |
| `platform.probes.readiness.periodSeconds` | PositiveInt |  | How often to probe, in seconds. |
| `platform.probes.readiness.initialDelaySeconds` | NonNegativeInt |  | How long to wait after the start before the first probe, in seconds. |
| `platform.probes.readiness.timeoutSeconds` | PositiveInt |  | How long one probe may take, in seconds. |
| `platform.probes.readiness.successThreshold` | PositiveInt |  | Consecutive successes that make the probe pass again. |
| `platform.probes.readiness.failureThreshold` | PositiveInt |  | Consecutive failures that make the probe fail. |
| `platform.probes.startup.path` | RootedPath |  | The path on the probe listener. Defaults to the contract's own. |
| `platform.probes.startup.periodSeconds` | PositiveInt |  | How often to probe, in seconds. |
| `platform.probes.startup.initialDelaySeconds` | NonNegativeInt |  | How long to wait after the start before the first probe, in seconds. |
| `platform.probes.startup.timeoutSeconds` | PositiveInt |  | How long one probe may take, in seconds. |
| `platform.probes.startup.successThreshold` | PositiveInt |  | Consecutive successes that make the probe pass again. |
| `platform.probes.startup.failureThreshold` | PositiveInt |  | Consecutive failures that make the probe fail. |
| `platform.drain.preStopSeconds` | NonNegativeInt |  | Fail readiness, then wait this long before the drain starts, so that whatever routes traffic has removed this endpoint first. Defaults to 5. |
| `platform.tls.csiDriver` | NonEmptyString |  | The driver that mounts the identity. The platform's, so there is no default; required once the identity is mounted. |
| `platform.tls.mountPath` | AbsPath |  | Defaults to /var/run/identity. |
| `platform.tls.mount` | boolean |  | Defaults to whether `config.tls.mode` is permissive or strict. True mounts the identity into a component that presents none of its own, because the release does; false never mounts it. |
| `platform.telemetry.serviceName` | NonEmptyString |  | Defaults to `<release>-<component>`. |
| `platform.telemetry.endpoint` | string |  | The collector to export to. Absent means do not export. |
| `platform.telemetry.protocol` | OtelProtocol |  | The protocol to export over. |
| `platform.telemetry.tracesSampler` | string |  | OpenTelemetry's `OTEL_TRACES_SAMPLER`. |
| `platform.telemetry.sampleRatio` | string \| number |  | The sampler's argument: a ratio, as a number or a string. |
| `platform.telemetry.resourceAttributes` | map of string |  | Attributes that describe the resource, name to value. |
| `platform.secrets.<name>.secretName` | NonEmptyString |  | The name of the Secret. |
| `platform.secrets.<name>.key` | NonEmptyString |  | The key within it. |
| `platform.env` | list of PlatformEnvVar |  | Environment a platform CLIENT LIBRARY reads (a database client's connection variables, for example), never the service's own configuration: a service takes no other structural input than its file (decision 0002 of the policy repository). A secret does not belong here; declare it in `secrets`. |
| `platform.volumes` | list of Named |  | Extra pod volumes, in Kubernetes' own shape, for what a client library mounts (a trust bundle, a password file). Open: passed through unchanged. |
| `platform.volumeMounts` | list of Mounted |  | The mounts for `volumes`, in Kubernetes' own shape. |
| `platform.config.fileName` | NonEmptyString |  | Defaults to `<component>.yaml`. |
| `platform.config.mountPath` | AbsPath |  | The directory the ConfigMap is mounted at. Defaults to `/etc/<chart name>`. |
| `platform.config.pathFlag` | NonEmptyString |  | The argument that carries the path. Defaults to `-config`. |
| `platform.config.pathEnv` | NonEmptyString |  | When set, the path is passed in this environment variable instead of an argument. |
| `platform.configMap.annotations` | map of string |  | For a ConfigMap that must be a hook resource: a pre-install job cannot mount one the release has not created yet. |
| `config.listen.address` | HostPort |  | **Required.** host:port, for example ":8080" or "127.0.0.1:8080". |
| `config.tls.mode` | TlsMode | `"off"` | 'off' serves cleartext only. 'permissive' serves BOTH, on two ports, so that an edge can migrate one side at a time without a coordinated window. 'strict' serves only the authenticated port. One listener cannot be both in every runtime, which is why permissive is two ports rather than one that sniffs. |
| `config.tls.address` | NonEmptyString |  | Where the authenticated listener binds under 'permissive', beside the cleartext one. Under 'strict' there is one listener and it is the service's own, so this is unused: the protocol changes, the address does not, and nothing downstream has to be told. |
| `config.tls.certFile` | NonEmptyString |  | The certificate this workload presents, mounted and rotated by the platform. Re-read when it changes, never cached for the process's lifetime: a one-hour certificate outlives no deployment. |
| `config.tls.keyFile` | NonEmptyString |  | Its private key. It lives in the pod and never in a secret, so a workload that can read secrets in its namespace still cannot read a neighbour's key. |
| `config.tls.caFile` | NonEmptyString |  | The authority peers are verified against, distributed by the platform as a trust bundle. |
| `config.tls.trustDomain` | NonEmptyString |  | The root of every identity this service will admit, for example 'example.internal'. A peer whose identity belongs to another trust domain is refused before its account is even considered. |
| `config.tls.peers` | list of TlsPeer |  | Who may call. Each entry is an ACCOUNT, not an address: an address resolves to whoever holds it today. An empty list admits no one, which is the correct default for a service nobody has been granted. |
| `config.greeting` | NonEmptyString |  | **Required.**  |
| `config.store.tokenEnv` | EnvName |  |  |
| `images.echo.registry` | string |  | The registry host. Left out, the repository is read as the whole name. |
| `images.echo.repository` | NonEmptyString |  | **Required.** The repository path, without the registry and without a tag. |
| `images.echo.tag` | string |  | The tag. Empty or absent when there is a digest. |
| `images.echo.digest` | ImageDigest |  | The content digest, `sha256:` and 64 hex digits; empty when there is none. |
