<!-- Generated from the chart's values contract. Do not edit. -->

## Values

| Key | Type | Default | Description |
|---|---|---|---|
| `installName` | string | `""` | The name this install is known by, folded into the JetStream stream and subject names. Empty means this chart's own release name. Letters, digits and hyphens only, at most 40 characters. |
| `tier` | "test" \| "primary" | `"test"` | Which kind of install this is. `test` provisions nothing of its own; `primary` provisions the objects this install owns. |
| `cloud.bucket` | string | `""` |  |
| `cloud.iamName` | string | `""` |  |
| `cloud.clusterName` | string | `""` |  |
| `cloud.accountID` | string | `""` |  |
| `cloud.region` | string | `""` |  |
| `cloud.serviceAccount` | string | `""` |  |
| `cloud.permissionsBoundary` | string | `""` |  |
| `cloud.project` | string | `""` | The project this install belongs to, tagged as `project` beside `cluster` on every AWS resource this chart creates. Empty means this release's own namespace — the platforms this chart targets already treat a namespace as a project. |
| `cloud.tags` | map of string | `Map()` | Extra tags folded into every AWS resource this chart creates, beside `cluster` and `project`. Neither of those two can be overridden from here. |
| `postgres.instances` | PositiveInt | `1` |  |
| `postgres.storage` | NonEmptyString | `"1Gi"` |  |
| `postgres.database` | NonEmptyString | `"url_shortener"` |  |
| `postgres.ownerRole` | NonEmptyString | `"url_shortener_owner"` |  |
| `postgres.runtimeRole` | NonEmptyString | `"url_shortener_app"` |  |
| `postgres.runtimePasswordSecret` | string | `""` |  |
| `postgres.tenantScopedNames` | boolean | `false` | Off by default, so every existing consumer keeps the fixed database and role names byte-for-byte. When true, `database`, `ownerRole` and `runtimeRole` are derived from this install's tenant scope instead, for whichever of the three a caller left at its own chart default; an explicit value always wins regardless of this flag. |
| `postgres.runtimePassword.generate` | boolean | `false` |  |
| `postgres.runtimePassword.length` | integer | `32` |  |
| `postgres.labels` | map of string | `Map()` |  |
| `postgres.platformOwned` | boolean | `false` |  |
| `postgres.annotations` | map of string | `Map()` |  |
| `postgres.scheduling.nodeSelector` | map of string | `Map()` |  |
| `postgres.scheduling.tolerations` | list of OpenObject | `[]` |  |
| `postgres.backup.objectStoreName` | string | `""` |  |
| `postgres.backup.serverName` | string | `""` |  |
| `postgres.serverTLS.secretName` | string | `""` |  |
| `postgres.serverTLS.caSecretName` | string | `""` |  |
| `events.storage` | "memory" \| "file" | `"memory"` |  |
| `events.url` | NonEmptyString | `"nats://nats.nats.svc:4222"` |  |
| `events.account` | string | `""` |  |
| `events.replicas` | PositiveInt | `1` |  |
| `events.maxAge` | string | `""` |  |
