<!-- Generated from the chart's values contract. Do not edit. -->

## Values

| Key | Type | Default | Description |
|---|---|---|---|
| `appRelease` | NonEmptyString |  | **Set at install.** The application release under test. Every install names its own. |
| `mode` | "full" \| "tenant" | `"full"` |  |
| `rolloutTimeout` | string | `""` |  |
| `database.host` | NonEmptyString |  | **Set at install.** The database host. Every install names its own. |
| `database.name` | NonEmptyString |  | **Set at install.** The database name. Every install names its own. |
| `database.owner.role` | NonEmptyString |  | **Set at install.** The owner role. Every install names its own. |
| `database.app.role` | NonEmptyString |  | **Set at install.** The role the service connects as. Every install names its own. |
| `database.app.passwordSecret` | NonEmptyString |  | **Set at install.** The Secret holding the role's password. Every install names its own. |
| `database.app.passwordKey` | NonEmptyString | `"password"` |  |
| `events.stream` | NonEmptyString |  | **Set at install.** Every name below is given by the install, never derived. |
| `events.redirectSubject` | NonEmptyString |  | **Set at install.**  |
| `events.requestSubject` | NonEmptyString |  | **Set at install.**  |
| `events.statConsumer` | NonEmptyString |  | **Set at install.**  |
| `events.logConsumer` | NonEmptyString |  | **Set at install.**  |
| `archive.bucket` | NonEmptyString |  | **Set at install.** The bucket, which exists already. Every install names its own. |
| `archive.region` | string | `""` |  |
| `archive.endpoint` | string | `""` |  |
| `archive.credentialsSecret` | string | `""` |  |
| `archive.accessKeyIDKey` | NonEmptyString | `"accessKeyID"` |  |
| `archive.secretAccessKeyKey` | NonEmptyString | `"secretAccessKey"` |  |
| `traces.url` | string | `""` |  |
| `traces.tokenExchange.tokenURL` | string | `""` |  |
| `traces.tokenExchange.client` | string | `""` |  |
| `traces.tokenExchange.audience` | NonEmptyString | `"access-issuer"` |  |
| `traces.tokenExchange.expirationSeconds` | PositiveInt | `3600` |  |
| `traces.caConfigMap` | string | `""` |  |
| `traces.caConfigMapKey` | NonEmptyString | `"ca-certificates.crt"` |  |
| `images.e2e.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.e2e.repository` | NonEmptyString | `"truvity/policy/url-shortener/e2e"` |  |
| `images.e2e.tag` | string | `""` |  |
| `images.e2e.digest` | string | `""` |  |
| `images.prober.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.prober.repository` | NonEmptyString | `"truvity/policy/url-shortener/prober"` |  |
| `images.prober.tag` | string | `""` |  |
| `images.prober.digest` | string | `""` |  |
| `pullPolicy` | PullPolicy | `"IfNotPresent"` |  |
| `job.ttlSecondsAfterFinished` | PositiveInt |  | OPTIONAL, and UNSET by default: a GitOps controller with self-heal on recreates a Job that deleted itself, so templates/job.yaml renders this key only when it is set. Absent or null skip it; a value must still clear the floor below. |
| `job.backoffLimit` | NonNegativeInt | `0` |  |
| `job.activeDeadlineSeconds` | PositiveInt | `300` |  |
| `job.annotations` | map of string | `{}` | Rendered on the Job's own metadata, never the pod template's — for a GitOps controller that must replace, not patch, an immutable Job when this chart's values change at the same version, e.g. a force/replace sync option. |
| `job.tls.enabled` | boolean | `false` |  |
| `resources` | OpenObject | `{"requests": {"cpu": "50m", "memory": "64Mi"}}` | Passed to Kubernetes verbatim, so the inside stays open: the platform validates it, and a chart that reimplements that schema goes stale. |
| `serviceAccount.create` | boolean | `true` |  |
| `serviceAccount.name` | string | `""` |  |
| `podSecurity.runAsUser` | NonNegativeInt | `65532` |  |
| `podSecurity.runAsGroup` | NonNegativeInt | `65532` |  |
| `podSecurity.fsGroup` | NonNegativeInt | `65532` |  |
| `prober.enabled` | boolean | `false` |  |
| `prober.interval` | NonEmptyString | `"10s"` |  |
| `prober.keyPrefix` | NonEmptyString | `"probe-"` |  |
| `prober.statSettle` | GoDuration | `"60s"` |  |
| `prober.pullPolicy` | PullPolicy | `"IfNotPresent"` |  |
| `prober.resources` | OpenObject | `{"requests": {"cpu": "10m", "memory": "32Mi"}}` | Passed to Kubernetes verbatim, so the inside stays open: the platform validates it, and a chart that reimplements that schema goes stale. |
| `prober.log.level` | LogLevel | `"info"` |  |
| `prober.drain.seconds` | PositiveInt | `5` |  |
| `prober.podSecurity.runAsUser` | NonNegativeInt | `65532` |  |
| `prober.podSecurity.runAsGroup` | NonNegativeInt | `65532` |  |
| `prober.podSecurity.fsGroup` | NonNegativeInt | `65532` |  |
| `prober.serviceAccount.create` | boolean | `true` |  |
| `prober.serviceAccount.name` | string | `""` |  |
| `tls.mode` | TlsMode | `"off"` |  |
| `tls.components.urls.mode` | TlsMode |  |  |
| `tls.components.redirect.mode` | GatewayTlsMode |  |  |
| `tls.trustDomain` | string | `""` |  |
| `tls.csiDriver` | NonEmptyString | `"spiffe.csi.cert-manager.io"` |  |
| `tls.mountPath` | NonEmptyString | `"/var/run/identity"` |  |
| `tls.grantRequest` | boolean | `true` |  |
| `tls.port` | Port | `8443` |  |
| `tls.peers` | list of UrlShortenerE2eTlsPeersItem | `[]` |  |
| `otel.endpoint` | string | `""` |  |
| `otel.protocol` | OtelProtocol | `"http/protobuf"` |  |
| `otel.tracesSampler` | NonEmptyString | `"parentbased_traceidratio"` |  |
| `otel.sampleRatio` | NonEmptyString | `"0.1"` |  |
| `otel.resourceAttributes` | map of string | `{}` |  |
