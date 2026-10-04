<!-- Generated from the chart's values contract. Do not edit. -->

## Values

| Key | Type | Default | Description |
|---|---|---|---|
| `service-lib.global` | OpenObject |  | Helm's own `global` block, passed through unchanged. |
| `installName` | string | `""` | The name this install is known by, folded into the NATS subject and durable consumer names. Empty means this chart's own release name. Letters, digits and hyphens only, at most 40 characters. |
| `tier` | Tier | `"test"` | Which kind of install this is, as url-shortener-infra's `tier`. `primary`: the identity ServiceAccount is rendered by url-shortener-infra, not here. `test`: this chart renders every account. |
| `alerts.enabled` | boolean | `false` |  |
| `alerts.ruleLabels` | map of string | `{}` |  |
| `alerts.alertLabels` | map of NonEmptyString | `{}` |  |
| `alerts.interval` | PromDuration |  |  |
| `alerts.remote.enabled` | boolean | `false` |  |
| `alerts.remote.clusterName` | DnsLabel |  |  |
| `alerts.remote.namespace` | DnsLabel |  |  |
| `alerts.prober.namespace` | DnsLabel |  |  |
| `alerts.journeyFailing.enabled` | boolean | `true` |  |
| `alerts.journeyFailing.for` | PromDuration | `"10m"` |  |
| `alerts.journeyFailing.ratio` | number | `0.5` |  |
| `alerts.journeyFailing.window` | PromDuration | `"15m"` |  |
| `alerts.proberAbsent.enabled` | boolean | `true` |  |
| `alerts.proberAbsent.for` | PromDuration | `"15m"` |  |
| `alerts.urlsRpcLatency.enabled` | boolean | `true` |  |
| `alerts.urlsRpcLatency.for` | PromDuration | `"15m"` |  |
| `alerts.urlsRpcLatency.seconds` | number | `1` |  |
| `alerts.urlsRpcErrors.enabled` | boolean | `true` |  |
| `alerts.urlsRpcErrors.for` | PromDuration | `"10m"` |  |
| `alerts.urlsRpcErrors.ratio` | number | `0.05` |  |
| `alerts.urlsRpcErrors.minCallsPerSecond` | number | `0.05` |  |
| `alerts.urlsRpcErrors.codes` | StatusCodeList | `"INTERNAL\|UNKNOWN\|UNAVAILABLE\|DATA_LOSS\|DEADLINE_EXCEEDED"` |  |
| `alerts.redirectHttp5xx.enabled` | boolean | `true` |  |
| `alerts.redirectHttp5xx.for` | PromDuration | `"10m"` |  |
| `alerts.redirectHttp5xx.ratio` | number | `0.05` |  |
| `alerts.redirectHttp5xx.minRequestsPerSecond` | number | `0.05` |  |
| `alerts.webHttp5xx.enabled` | boolean | `true` |  |
| `alerts.webHttp5xx.for` | PromDuration | `"10m"` |  |
| `alerts.webHttp5xx.ratio` | number | `0.05` |  |
| `alerts.webHttp5xx.minRequestsPerSecond` | number | `0.05` |  |
| `alerts.redirectLatency.enabled` | boolean | `true` |  |
| `alerts.redirectLatency.for` | PromDuration | `"15m"` |  |
| `alerts.redirectLatency.seconds` | number | `0.5` |  |
| `alerts.webLatency.enabled` | boolean | `true` |  |
| `alerts.webLatency.for` | PromDuration | `"15m"` |  |
| `alerts.webLatency.seconds` | number | `2` |  |
| `database.host` | NonEmptyString |  | The database host. Every install names its own. |
| `database.name` | NonEmptyString | `"url_shortener"` |  |
| `database.clusterDomain` | NonEmptyString | `"cluster.local"` |  |
| `database.tls.mode` | "verify-full" | `"verify-full"` | Only verify-full: the database client verifies the server against the root below, or does not connect. Kept so a platform that already sets it keeps rendering; `require` is refused. |
| `database.tls.rootCA.configMapName` | string | `""` | The ConfigMap holding the root the database server certificate chains to. Required. |
| `database.tls.rootCA.key` | NonEmptyString | `"ca-certificates.crt"` |  |
| `database.owner.role` | NonEmptyString | `"url_shortener_owner"` |  |
| `database.owner.passwordSecret` | NonEmptyString |  | The Secret holding the role's password. Every install names its own. |
| `database.owner.passwordKey` | NonEmptyString | `"password"` |  |
| `database.app.role` | NonEmptyString | `"url_shortener_app"` |  |
| `database.app.passwordSecret` | NonEmptyString |  | The Secret holding the role's password. Every install names its own. |
| `database.app.passwordKey` | NonEmptyString | `"password"` |  |
| `events.url` | NonEmptyString |  | The events server's URL. Every install names its own. |
| `events.auth.audience` | string | `""` |  |
| `events.auth.expirationSeconds` | PositiveInt | `3600` |  |
| `events.tls.enabled` | boolean | `false` |  |
| `events.tls.serverName` | string | `""` |  |
| `events.tls.caConfigMap` | string | `""` |  |
| `events.tls.caKey` | NonEmptyString | `"ca-certificates.crt"` |  |
| `archive.bucket.name` | NonEmptyString |  | The bucket, which exists already. Every install names its own. |
| `archive.bucket.region` | string | `""` |  |
| `archive.bucket.endpoint` | string | `""` |  |
| `archive.bucket.ca` | string | `""` |  |
| `archive.bucket.pathStyle` | boolean | `false` |  |
| `archive.bucket.credentialsSecret` | string | `""` | The Secret holding static credentials. Empty means the SDK's ambient credentials, which is what a workload identity provides. |
| `archive.bucket.accessKeyIDKey` | NonEmptyString | `"accessKeyID"` |  |
| `archive.bucket.secretAccessKeyKey` | NonEmptyString | `"secretAccessKey"` |  |
| `archive.prefix` | NonEmptyString | `"url-shortener/requests"` |  |
| `archive.batch.maxRecords` | PositiveInt | `500` |  |
| `archive.batch.maxSeconds` | PositiveInt | `60` |  |
| `log.level` | LogLevel | `"info"` |  |
| `replicas.redirect` | NonNegativeInt | `2` |  |
| `replicas.stat` | NonNegativeInt | `2` |  |
| `replicas.log` | NonNegativeInt | `2` |  |
| `replicas.urls` | NonNegativeInt | `2` |  |
| `replicas.web` | NonNegativeInt | `2` |  |
| `resources.redirect` | OpenObject | `{"requests": {"cpu": "50m", "memory": "64Mi"}}` |  |
| `resources.stat` | OpenObject | `{"requests": {"cpu": "50m", "memory": "64Mi"}}` |  |
| `resources.migrate` | OpenObject | `{"requests": {"cpu": "50m", "memory": "64Mi"}}` |  |
| `resources.log` | OpenObject | `{"requests": {"cpu": "50m", "memory": "128Mi"}}` |  |
| `resources.urls` | OpenObject | `{"requests": {"cpu": "50m", "memory": "64Mi"}}` |  |
| `resources.web` | OpenObject | `{"requests": {"cpu": "50m", "memory": "96Mi"}}` |  |
| `resources.verify` | OpenObject | `{"requests": {"cpu": "50m", "memory": "64Mi"}}` |  |
| `route.enabled` | boolean | `false` |  |
| `route.hostname` | string | `""` |  |
| `route.parentRef.name` | string | `""` |  |
| `route.parentRef.namespace` | string | `""` |  |
| `route.parentRef.sectionName` | string | `""` |  |
| `route.parentRef.group` | string | `""` |  |
| `route.parentRef.kind` | string | `""` |  |
| `route.ruleName` | NonEmptyString | `"app"` |  |
| `route.faro.enabled` | boolean | `false` |  |
| `route.faro.ruleName` | NonEmptyString | `"faro"` |  |
| `route.faro.path` | UrlPath | `"/faro/collect"` |  |
| `route.faro.rewritePath` | UrlPathOrEmpty | `""` |  |
| `route.faro.requestBufferLimit` | BufferLimit |  | The largest request body the gateway accepts on the telemetry rule, as a quantity such as 256Ki. Absent renders no policy. |
| `route.faro.backend.name` | string | `""` |  |
| `route.faro.backend.namespace` | string | `""` |  |
| `route.faro.backend.port` | NonNegativeInt | `0` |  |
| `route.redirectRuleName` | NonEmptyString | `"redirect"` | The rule that resolves short links. Separate from ruleName so a policy can protect the site without locking the public resolver. |
| `serviceAccount.create` | boolean | `true` |  |
| `serviceAccount.app.name` | string | `""` |  |
| `serviceAccount.app.annotations` | map of string | `{}` |  |
| `serviceAccount.migrate.name` | string | `""` |  |
| `serviceAccount.migrate.annotations` | map of string | `{}` |  |
| `serviceAccount.components.redirect.name` | DnsName |  |  |
| `serviceAccount.components.redirect.annotations` | map of string |  |  |
| `serviceAccount.components.urls.name` | DnsName |  |  |
| `serviceAccount.components.urls.annotations` | map of string |  |  |
| `serviceAccount.components.web.name` | DnsName |  |  |
| `serviceAccount.components.web.annotations` | map of string |  |  |
| `serviceAccount.components.stat.name` | DnsName |  |  |
| `serviceAccount.components.stat.annotations` | map of string |  |  |
| `serviceAccount.components.log.name` | DnsName |  |  |
| `serviceAccount.components.log.annotations` | map of string |  |  |
| `web.csp.mode` | CspMode | `"report-only"` |  |
| `web.csp.connectSrc` | list of HttpOrigin | `[]` |  |
| `web.csp.reportUri` | ReportUri | `""` |  |
| `web.faro.enabled` | boolean | `false` |  |
| `web.faro.collectorUrl` | CollectorUrl | `"/faro/collect"` |  |
| `web.faro.apiKey` | PublicKey | `""` |  |
| `web.faro.appName` | string | `"url-shortener-web"` |  |
| `web.faro.environment` | string | `""` |  |
| `web.faro.sampleRate` | Ratio | `1` |  |
| `drain.seconds` | PositiveInt | `20` |  |
| `drain.preStopSeconds` | NonNegativeInt | `5` |  |
| `disruption.enabled` | boolean | `true` |  |
| `disruption.maxUnavailable` | NonNegativeInt | `1` |  |
| `tls.mode` | TlsMode | `"off"` | The release-wide default for the components that serve an authenticated boundary. `strict` is accepted here only where `redirect` is overridden below: redirect is gateway-fronted and can never be strict, so the render refuses it when inherited. To make only the URL service strict, set `permissive` (or `off`) here and `tls.components.urls.mode: strict`. |
| `tls.components.urls.mode` | TlsMode |  |  |
| `tls.components.redirect.mode` | GatewayTlsMode |  |  |
| `tls.trustDomain` | string | `""` |  |
| `tls.csiDriver` | NonEmptyString | `"spiffe.csi.cert-manager.io"` |  |
| `tls.mountPath` | NonEmptyString | `"/var/run/identity"` |  |
| `tls.grantRequest` | boolean | `true` |  |
| `tls.port` | Port | `8443` |  |
| `tls.peers.redirect` | list of UrlShortenerTlsPeersRedirectItem | `[]` |  |
| `tls.peers.urls` | list of UrlShortenerTlsPeersRedirectItem | `[]` |  |
| `podSecurity.runAsUser` | NonNegativeInt | `65532` |  |
| `podSecurity.runAsGroup` | NonNegativeInt | `65532` |  |
| `podSecurity.fsGroup` | NonNegativeInt | `65532` |  |
| `images.migrate.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.migrate.repository` | NonEmptyString | `"truvity/policy/url-shortener/migrate"` |  |
| `images.migrate.tag` | string | `""` |  |
| `images.migrate.digest` | string | `""` |  |
| `images.redirect.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.redirect.repository` | NonEmptyString | `"truvity/policy/url-shortener/redirect"` |  |
| `images.redirect.tag` | string | `""` |  |
| `images.redirect.digest` | string | `""` |  |
| `images.urls.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.urls.repository` | NonEmptyString | `"truvity/policy/url-shortener/urls"` |  |
| `images.urls.tag` | string | `""` |  |
| `images.urls.digest` | string | `""` |  |
| `images.stat.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.stat.repository` | NonEmptyString | `"truvity/policy/url-shortener/stat"` |  |
| `images.stat.tag` | string | `""` |  |
| `images.stat.digest` | string | `""` |  |
| `images.log.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.log.repository` | NonEmptyString | `"truvity/policy/url-shortener/log"` |  |
| `images.log.tag` | string | `""` |  |
| `images.log.digest` | string | `""` |  |
| `images.web.registry` | NonEmptyString | `"ghcr.io"` |  |
| `images.web.repository` | NonEmptyString | `"truvity/policy/url-shortener/web"` |  |
| `images.web.tag` | string | `""` |  |
| `images.web.digest` | string | `""` |  |
| `pullPolicy` | PullPolicy | `"IfNotPresent"` |  |
| `otel.endpoint` | string | `""` |  |
| `otel.protocol` | OtelProtocol | `"http/protobuf"` |  |
| `otel.tracesSampler` | NonEmptyString | `"parentbased_traceidratio"` |  |
| `otel.sampleRatio` | NonEmptyString | `"0.1"` |  |
| `otel.resourceAttributes` | map of string | `{}` |  |
