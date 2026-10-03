<!-- Generated from the chart's values contract. Do not edit. -->

## Values

| Key | Type | Default | Description |
|---|---|---|---|
| `appRelease` | NonEmptyString |  | **Required.**  |
| `mode` | "full" \| "tenant" | `"full"` |  |
| `rolloutTimeout` | string | `""` |  |
| `database.host` | NonEmptyString |  | **Required.**  |
| `database.name` | NonEmptyString |  | **Required.**  |
| `database.owner.role` | NonEmptyString |  | **Required.**  |
| `database.app.role` | NonEmptyString |  | **Required.**  |
| `database.app.passwordSecret` | NonEmptyString |  | **Required.**  |
| `database.app.passwordKey` | NonEmptyString | `"password"` |  |
| `events.stream` | NonEmptyString |  | **Required.**  |
| `events.redirectSubject` | NonEmptyString |  | **Required.**  |
| `events.requestSubject` | NonEmptyString |  | **Required.**  |
| `events.statConsumer` | NonEmptyString |  | **Required.**  |
| `events.logConsumer` | NonEmptyString |  | **Required.**  |
| `archive.bucket` | NonEmptyString |  | **Required.**  |
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
| `job.ttlSecondsAfterFinished` | integer |  | OPTIONAL, and UNSET by default: a GitOps controller with self-heal on recreates a Job that deleted itself, so templates/job.yaml renders this key only when it is set. Absent or null skip it; a value must still clear the floor below. |
| `job.backoffLimit` | NonNegativeInt | `0` |  |
| `job.activeDeadlineSeconds` | PositiveInt | `300` |  |
| `job.annotations` | map of string | `Map()` | Rendered on the Job's own metadata, never the pod template's — for a GitOps controller that must replace, not patch, an immutable Job when this chart's values change at the same version, e.g. a force/replace sync option. |
| `job.tls.enabled` | boolean | `false` |  |
| `resources` | OpenObject |  | **Required.** Passed to Kubernetes verbatim, so the inside stays open: the platform validates it, and a chart that reimplements that schema goes stale. |
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
| `prober.resources` | OpenObject |  | Passed to Kubernetes verbatim, so the inside stays open: the platform validates it, and a chart that reimplements that schema goes stale. |
| `prober.log.level` | LogLevel | `"info"` |  |
| `prober.drain.seconds` | PositiveInt | `5` |  |
| `prober.podSecurity.runAsUser` | NonNegativeInt | `65532` |  |
| `prober.podSecurity.runAsGroup` | NonNegativeInt | `65532` |  |
| `prober.podSecurity.fsGroup` | NonNegativeInt | `65532` |  |
| `prober.serviceAccount.create` | boolean | `true` |  |
| `prober.serviceAccount.name` | string | `""` |  |
| `tls.mode` | TlsMode | `"off"` |  |
| `tls.components.urls.mode` | TlsMode |  |  |
| `tls.components.redirect.mode` | "off" \| "permissive" |  |  |
| `tls.trustDomain` | string | `""` |  |
| `tls.csiDriver` | NonEmptyString | `"spiffe.csi.cert-manager.io"` |  |
| `tls.mountPath` | NonEmptyString | `"/var/run/identity"` |  |
| `tls.grantRequest` | boolean | `true` |  |
| `tls.port` | Port | `8443` |  |
| `tls.peers` | list of UrlShortenerE2eTlsPeersItem | `[]` |  |
| `otel.endpoint` | string | `""` |  |
| `otel.protocol` | "http/protobuf" \| "grpc" | `"http/protobuf"` |  |
| `otel.tracesSampler` | NonEmptyString | `"parentbased_traceidratio"` |  |
| `otel.sampleRatio` | NonEmptyString | `"0.1"` |  |
| `otel.resourceAttributes` | map of string | `Map()` |  |
