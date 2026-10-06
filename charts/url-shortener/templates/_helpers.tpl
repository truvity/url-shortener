{{/*
The install's name, which every object here is prefixed with. Two installs
in one namespace must not collide, and a name derived from the release is
the only thing that guarantees it.
*/}}
{{- define "url-shortener.name" -}}
{{- .Release.Name -}}
{{- end -}}

{{/*
app.kubernetes.io/version is this chart's OWN version — never a value,
because the charts release under one version together (see
.github/workflows/release.yaml's own comment on why url-shortener and
url-shortener-infra ship as one) and a value could
disagree with what the release actually stamped. Quoted: an appVersion
that happens to look like a number (e.g. a bare "2") must still render as
a label VALUE, not a YAML integer metadata.labels rejects.

Never in a Deployment's own selector — see
service-lib's selectorLabels and
templates/poddisruptionbudget.yaml, which build their selectors from
name/instance/component alone: a selector that included this would stop
matching the incumbent replicas the moment a rollout changed it, which is
the opposite of what a rolling update needs.

This is also the ONE label examples/url-shortener/e2e's own suite reads
off a live pod to prove a test is judging the version that was actually
promoted — see e2e/suite/readiness_test.go's waitForPromotedRollout and the
E2E_APP_VERSION a product's own CI hands the suite.
*/}}
{{- define "url-shortener.labels" -}}
app.kubernetes.io/name: url-shortener
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{/*
The database connection, as the libpq environment the platform's PostgreSQL
client library reads (the same names the database chart's `cnpg-client.env`
helper renders): host, port, database, role, and the CA file. Nothing here is a connection URL. A URL is a string a parameter can
be dropped from on its way to the driver, and a dropped `sslrootcert` turns
verify-full into a connection that does not verify; the client takes parts and
refuses anything weaker than verify-full.

That helper is not included itself: it mounts a CLIENT CERTIFICATE from a
Secret named for the cluster, and this chart's roles log in with a password
against a root the platform hands in as a ConfigMap. The names are the same;
the sources differ.

Takes (dict "root" $ "role" <.Values.database.owner|app> "component" <name>).
The password is a FILE, not a variable: the client re-reads it for every new
connection, so a rotated Secret reaches a running pod within one connection
lifetime without a restart. See dbPasswordVolume.
*/}}
{{- define "url-shortener.dbEnv" -}}
{{- $root := .root -}}
- name: PGHOST
  value: {{ include "url-shortener.dbHost" $root | quote }}
- name: PGPORT
  value: "5432"
- name: PGDATABASE
  value: {{ $root.Values.database.name | quote }}
- name: PGUSER
  value: {{ .role.role | quote }}
- name: PGSSLMODE
  value: verify-full
- name: PGSSLROOTCERT
  value: {{ include "url-shortener.dbCAMountPath" $root }}/{{ $root.Values.database.tls.rootCA.key | default "ca-certificates.crt" }}
- name: CNPG_CLIENT_PASSWORD_FILE
  value: {{ include "url-shortener.dbPasswordMountPath" $root }}/password
- name: PGAPPNAME
  value: {{ printf "%s-%s" (include "url-shortener.name" $root) .component | quote }}
{{- end -}}

{{/*
dbTLSMode: verify-full, always. The client library refuses anything weaker,
so a chart that could still render `require` would render a Deployment that
cannot start. `require` is refused here with the reason, and the root the
server certificate chains to is required: the platform provides it, the chart
never creates it.

The value is kept (it is what a platform that set it already passes) and
accepts only the one setting it ever reaches.
*/}}
{{- define "url-shortener.dbTLSMode" -}}
{{- $mode := .Values.database.tls.mode | default "verify-full" -}}
{{- if ne $mode "verify-full" -}}
{{- fail (printf "database.tls.mode %q is refused: the database client verifies the server (verify-full) or does not connect. Unset it, and give database.tls.rootCA.configMapName" $mode) -}}
{{- end -}}
{{- if not .Values.database.tls.rootCA.configMapName -}}
{{- fail "database.tls.rootCA.configMapName is required: the ConfigMap holding the root the database server certificate chains to (the client verifies it, always)" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{/*
dbHost: the name the server certificate carries is the fully-qualified one
only, so a short name becomes `<host>.<namespace>.svc.<clusterDomain>`. A host
that already has a dot is taken to be qualified and left alone.
*/}}
{{- define "url-shortener.dbHost" -}}
{{- $_ := include "url-shortener.dbTLSMode" . -}}
{{- if not (contains "." .Values.database.host) -}}
{{- printf "%s.%s.svc.%s" .Values.database.host .Release.Namespace (.Values.database.clusterDomain | default "cluster.local") -}}
{{- else -}}
{{- .Values.database.host -}}
{{- end -}}
{{- end -}}

{{/*
dbCAMountPath: where the root is mounted, as a DIRECTORY (never a subPath,
which a rotation of the ConfigMap would not reach).
*/}}
{{- define "url-shortener.dbCAMountPath" -}}
/etc/url-shortener-pg-ca
{{- end -}}

{{/*
dbPasswordMountPath: where a role's password is mounted, as a DIRECTORY
holding one file, `password`. A directory, never a subPath, for the same
reason as the root: a rotated Secret reaches a running pod.
*/}}
{{- define "url-shortener.dbPasswordMountPath" -}}
/etc/url-shortener-pg-password
{{- end -}}

{{- define "url-shortener.dbCAVolume" -}}
{{- $_ := include "url-shortener.dbTLSMode" . -}}
- name: database-ca
  configMap:
    name: {{ .Values.database.tls.rootCA.configMapName }}
{{- end -}}

{{- define "url-shortener.dbCAMount" -}}
- name: database-ca
  mountPath: {{ include "url-shortener.dbCAMountPath" . }}
  readOnly: true
{{- end -}}

{{/*
A role's password, as a file from the Secret that holds it.

The chart takes the NAME of a secret, never a value: a chart that generated a
password would put it in the release's own stored manifest, where anyone who
can read a release can read the password. The credential is the same Secret
and key as before; only how it reaches the process changed (a file, which the
client re-reads, instead of a variable, which is read once).

Takes the role block (.Values.database.owner or .Values.database.app), so the
migration and the services cannot accidentally be handed the same credential.
*/}}
{{- define "url-shortener.dbPasswordVolume" -}}
- name: database-password
  secret:
    secretName: {{ .passwordSecret }}
    defaultMode: 0440
    items:
      - key: {{ .passwordKey | default "password" }}
        path: password
{{- end -}}

{{- define "url-shortener.dbPasswordMount" -}}
- name: database-password
  mountPath: {{ include "url-shortener.dbPasswordMountPath" . }}
  readOnly: true
{{- end -}}

{{/*
The account ONE component runs as. EVERY component has its own, always:
`redirect`, `urls`, `web`, `stat` and `log` never share an account, because a
workload identity is namespace plus account, and a shared account makes two
components indistinguishable to an allow-list.

The order: `serviceAccount.components.<component>.name`; for `log` only,
`serviceAccount.app.name` (the account a platform already bound a cloud role
to for the archive bucket, which is still log's own account: nothing else
runs as it); then `<release>-<component>`.

Takes the root context and the component name.
*/}}
{{- define "url-shortener.componentServiceAccountName" -}}
{{- $sa := .root.Values.serviceAccount -}}
{{- $own := (get ($sa.components | default dict) .component | default dict).name -}}
{{- if $own -}}
{{- $own -}}
{{- else if and (eq .component "log") $sa.app.name -}}
{{- $sa.app.name -}}
{{- else -}}
{{- printf "%s-%s" (include "url-shortener.name" .root) .component -}}
{{- end -}}
{{- end -}}

{{/*
Whether `log`'s account is the IDENTITY account, which url-shortener-infra
renders on a primary install (delivery interface step 13) and this chart
therefore does not.

`tier` is the one url-shortener-infra reads, with the same two values and the
same meaning, and the platform hands both charts the same one. A `test`
install has no Pod Identity association to wait for, so this chart renders its
account as it always did.

Renders "true" or nothing.
*/}}
{{- define "url-shortener.identityInInfra" -}}
{{- if eq .Values.tier "primary" -}}
true
{{- end -}}
{{- end -}}

{{/*
Refuses two workloads sharing an account: an identity must name ONE
component, and a shared name quietly turns an allow-list for `web` into one
for `stat` too. The migration counts, so its rights stay off the request path.
*/}}
{{- define "url-shortener.checkComponentAccounts" -}}
{{- $seen := dict -}}
{{- range (list "redirect" "urls" "web" "stat" "log") -}}
{{- $n := include "url-shortener.componentServiceAccountName" (dict "root" $ "component" .) -}}
{{- if hasKey $seen $n -}}
{{- fail (printf "serviceAccount: %s and %s would both run as %q; every component needs its own account, or the identity cannot tell them apart" (get $seen $n) . $n) -}}
{{- end -}}
{{- $_ := set $seen $n . -}}
{{- end -}}
{{- if .Values.prober.enabled -}}
{{- $pr := include "url-shortener.proberServiceAccountName" . -}}
{{- if hasKey $seen $pr -}}
{{- fail (printf "serviceAccount: %s and the prober would both run as %q; the prober needs an account of its own, so an allow-list can name it alone" (get $seen $pr) $pr) -}}
{{- end -}}
{{- end -}}
{{- $m := include "url-shortener.migrateServiceAccountName" . -}}
{{- if hasKey $seen $m -}}
{{- fail (printf "serviceAccount: %s and the migration would both run as %q; the migration's rights must stay off the request path" (get $seen $m) $m) -}}
{{- end -}}
{{- end -}}

{{- define "url-shortener.migrateServiceAccountName" -}}
{{- .Values.serviceAccount.migrate.name | default (printf "%s-migrate" (include "url-shortener.name" .)) -}}
{{- end -}}

{{/*
Whether the transport is authenticated at all. Everything TLS-shaped in this
chart is behind this, so that the default render is byte-identical to one
from a chart that had never heard of it — which is what a golden proves.
*/}}
{{- define "url-shortener.tlsOn" -}}
{{- /* Both modes are read BEFORE `or` sees either: `or` stops at the first true operand, and a strict redirect must be refused even when urls already said on. */ -}}
{{- $urls := include "url-shortener.tlsMode" (dict "root" . "component" "urls") -}}
{{- $redirect := include "url-shortener.tlsMode" (dict "root" . "component" "redirect") -}}
{{- if or (ne $urls "off") (ne $redirect "off") }}yes{{ end -}}
{{- end -}}

{{/*
The transport mode ONE serving component runs in: its own
`tls.components.<name>.mode` when set, otherwise the release-wide `tls.mode`.

Only the two components that SERVE an authenticated boundary have a mode of
their own: `urls`, called in-cluster by web, stat and the test Job, and
`redirect`. `web` and `stat` only call out, so for them there is no third
state and no mode to choose (see _components.tpl).

`redirect` is refused `strict`, whichever way it got there. It is fronted by
a gateway, which terminates TLS at the edge and forwards cleartext, so a
strict redirect would refuse the only caller it has. The schema refuses it
written down; this refuses it INHERITED, which a schema cannot see — a
release-wide `tls.mode: strict` with no override for redirect.
*/}}
{{- define "url-shortener.tlsMode" -}}
{{- $components := .root.Values.tls.components | default dict -}}
{{- $own := get (get $components .component | default dict) "mode" -}}
{{- $mode := $own | default .root.Values.tls.mode -}}
{{- if and (eq .component "redirect") (eq $mode "strict") -}}
{{- fail "redirect cannot be strict: it is fronted by a gateway that terminates TLS and forwards cleartext, so a strict redirect refuses its only caller. Set tls.mode to permissive (or off) and tls.components.urls.mode to strict to make only the URL service strict" -}}
{{- end -}}
{{- $mode -}}
{{- end -}}

{{/*
Whether the components that only CALL the URL service (`web`, `stat`)
present an identity: exactly when the URL service authenticates at all.
*/}}
{{- define "url-shortener.urlsClientTLSOn" -}}
{{- if ne (include "url-shortener.tlsMode" (dict "root" . "component" "urls")) "off" }}yes{{ end -}}
{{- end -}}

{{/*
The token a component authenticates to the broker with.

A PROJECTED ServiceAccount token, which the pod cannot forge and which
expires on its own — not a secret mounted from somewhere, and not a
credential this chart or its values ever hold. The platform says which
audience the broker demands; everything else follows from that.

Empty audience renders nothing, which is a broker that admits anonymous
clients. That is what a local one does, and what a laptop needs.
*/}}
{{/*
Whether redirect authenticates to the broker with its workload identity
(`events.tls.enabled`) instead of a token. It needs the identity to exist, so
it is refused where redirect has none: TLS to a broker that verifies clients,
with no client certificate, fails at the handshake with an error that names
neither setting.
*/}}
{{- define "url-shortener.eventsIdentityOn" -}}
{{- if .Values.events.tls.enabled -}}
{{- if eq (include "url-shortener.tlsMode" (dict "root" . "component" "redirect")) "off" -}}
{{- fail "events.tls.enabled presents redirect's workload identity to the broker, but redirect has none: set tls.mode (or tls.components.redirect.mode) to permissive so an identity is mounted" -}}
{{- end -}}
{{- if not .Values.events.tls.caConfigMap -}}
{{- fail "events.tls.caConfigMap is required when events.tls.enabled: the trust bundle the broker's certificate is verified against" -}}
{{- end -}}
yes
{{- end -}}
{{- end -}}

{{/*
The broker's trust bundle, mounted into redirect when it authenticates with
its identity. A ConfigMap volume: the platform writes it, this only reads it.
*/}}
{{- define "url-shortener.eventsCAVolume" -}}
{{- if include "url-shortener.eventsIdentityOn" . }}
- name: events-ca
  configMap:
    name: {{ .Values.events.tls.caConfigMap }}
{{- end }}
{{- end -}}

{{- define "url-shortener.eventsCAMount" -}}
{{- if include "url-shortener.eventsIdentityOn" . }}
- name: events-ca
  mountPath: /var/run/events-ca
  readOnly: true
{{- end }}
{{- end -}}

{{- define "url-shortener.eventsTokenVolume" -}}
{{- with .Values.events.auth.audience -}}
- name: events-token
  projected:
    sources:
      - serviceAccountToken:
          audience: {{ . | quote }}
          expirationSeconds: {{ $.Values.events.auth.expirationSeconds | default 3600 }}
          path: token
{{- end }}
{{- end -}}

{{- define "url-shortener.eventsTokenMount" -}}
{{- with .Values.events.auth.audience -}}
- name: events-token
  mountPath: /var/run/events
  readOnly: true
{{- end }}
{{- end -}}

{{/*
The name THIS install is known by, across both charts of the pair.

Mirrors url-shortener-infra's "url-shortener-infra.installName" exactly,
because it exists to answer the same question the other chart answers for
itself: what is this release called, for the purpose of the stream it
connects to. Defaulting to this chart's own release name is what makes a
standalone install work with nothing set; a platform giving the two
releases different names sets this to the infrastructure release's own
installName, on both charts, identically.

REFUSED, not sanitised, when it is not a safe shape — see the other
chart's helper for why: this is a plain string a caller can set to
anything, unlike `.Release.Namespace` and `.Release.Name`, and it is
folded into the NATS subject and durable names below.
*/}}
{{- define "url-shortener.installName" -}}
{{- $name := .Values.installName | default .Release.Name -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$" $name) -}}
{{- fail (printf "installName %q must be a lowercase name of letters, digits and hyphens, at most 40 characters: it is folded into a NATS subject and durable consumer name, which is why this refuses it instead of lower-casing or truncating it for you" $name) -}}
{{- end -}}
{{- $name -}}
{{- end -}}

{{/*
The tenant scope every cluster-global name below derives from.

MUST MATCH url-shortener-infra/templates/_helpers.tpl's
"url-shortener-infra.eventsScope" — read that comment for why namespace
and install name together are the smallest pair that separates both
collision shapes, and why this is computed rather than taken as a value
(platform.md's naming rule; the name is this project's own convention,
not a platform's).
*/}}
{{- define "url-shortener.eventsScope" -}}
{{- printf "%s-%s" .Release.Namespace (include "url-shortener.installName" .) -}}
{{- end -}}

{{/*
The JetStream stream this chart CONNECTS to (platform.md rule 6, "found,
not made") and the two subjects it reads and writes on it. Computed by the
SAME formula as url-shortener-infra's "url-shortener-infra.eventsStream",
"...redirectSubject" and "...requestSubject" — not passed as a value,
because agreement by formula is what makes the two charts's names equal
without either release knowing the other's.
*/}}
{{- define "url-shortener.eventsStream" -}}
{{- printf "%s-events" (include "url-shortener.eventsScope" .) -}}
{{- end -}}

{{- define "url-shortener.redirectSubject" -}}
{{- printf "%s.redirect" (include "url-shortener.eventsScope" .) -}}
{{- end -}}

{{- define "url-shortener.requestSubject" -}}
{{- printf "%s.log" (include "url-shortener.eventsScope" .) -}}
{{- end -}}

{{/*
Durable consumer names, one per consuming component.

NATS only requires a durable name to be unique WITHIN its stream, and
this install's stream is already scoped to it — a durable name of just
"stat" would not collide on the broker. It is scoped anyway, because
every other cluster-global name here comes from one formula, and a
component suffix is cheap to add beside it: one rule for the whole
family is one fewer thing a reader has to remember is the exception.
This pair is internal to this chart alone — the infrastructure chart
renders no consumer, so there is nothing on its side to match.
*/}}
{{- define "url-shortener.statConsumer" -}}
{{- printf "%s-stat" (include "url-shortener.eventsScope" .) -}}
{{- end -}}

{{- define "url-shortener.logConsumer" -}}
{{- printf "%s-log" (include "url-shortener.eventsScope" .) -}}
{{- end -}}

{{/*
Whether this render is the alert rules ALONE (alerts.remote.enabled): an install
that runs on another cluster and is evaluated where the shared store's
evaluator runs needs the rules and nothing else, so every other template here
renders nothing under it. Empty when off.
*/}}
{{- define "url-shortener.alertsRemote" -}}
{{- if and .Values.alerts .Values.alerts.remote .Values.alerts.remote.enabled -}}true{{- end -}}
{{- end -}}

{{/*
The label matchers every alert expression starts with: the install's
namespace on the cluster that runs it and, remotely, the cluster itself (the
shared store holds many). Takes (dict "root" $ "namespace" <the namespace>).
*/}}
{{- define "url-shortener.alertSelector" -}}
{{- if include "url-shortener.alertsRemote" .root -}}
k8s_cluster_name="{{ .root.Values.alerts.remote.clusterName }}", {{ end -}}
k8s_namespace_name="{{ .namespace }}"
{{- end -}}

{{/*
What every alert carries: the severity it sets, the cluster when the rule is
evaluated away from it, and the platform's own labels (alerts.alertLabels).
Takes (dict "severity" "cluster" "extra").
*/}}
{{- define "url-shortener.alertLabels" -}}
severity: {{ .severity }}
{{- if .cluster }}
k8s_cluster_name: {{ .cluster | quote }}
{{- end }}
{{- range $k, $v := .extra }}
{{ $k }}: {{ $v | quote }}
{{- end }}
{{- end -}}

{{/*
How much redundancy the cluster wants, as the platform states it (delivery
interface step 14): `single` or `high`. Absent means `single`; anything else is
refused, so a typo cannot quietly give a cluster the wrong number of replicas.
*/}}
{{- define "url-shortener.availability" -}}
{{- $a := .Values.availability | default "single" -}}
{{- if not (has $a (list "single" "high")) -}}
{{- fail (printf "availability %q is not one of single, high" $a) -}}
{{- end -}}
{{- $a -}}
{{- end -}}

{{/*
The replicas of ONE component: an explicit `replicas.<component>` wins, and
otherwise `availability` decides (single: one, high: two). Takes
(dict "root" $ "component" <name>).
*/}}
{{- define "url-shortener.replicas" -}}
{{- $replicas := .root.Values.replicas | default dict -}}
{{- $own := get $replicas .component -}}
{{- if not (hasKey $replicas .component) -}}
{{- ternary 2 1 (eq (include "url-shortener.availability" .root) "high") -}}
{{- else -}}
{{- $own -}}
{{- end -}}
{{- end -}}

{{/*
Whether the PodDisruptionBudgets render: an explicit `disruption.enabled` wins,
and otherwise `availability: high` does. A budget on a single replica would
only wedge the drain of its node, so `single` renders none.
*/}}
{{- define "url-shortener.disruptionOn" -}}
{{- $disruption := .Values.disruption | default dict -}}
{{- $en := get $disruption "enabled" -}}
{{- if not (hasKey $disruption "enabled") -}}
{{- if eq (include "url-shortener.availability" .) "high" }}yes{{ end -}}
{{- else if $en }}yes{{ end -}}
{{- end -}}

{{/*
The credential of the schema migration, and the ONLY route the owner's password
takes into this chart (delivery interface step 15): `database.migration.*`,
read by the migration Job and by nothing else. `database.owner.*` is the
deprecated spelling of the same thing and is read only when `migration` leaves
the key unset. Renders JSON: role, passwordSecret, passwordKey.
*/}}
{{- define "url-shortener.migrationDatabase" -}}
{{- $m := .Values.database.migration | default dict -}}
{{- $o := .Values.database.owner | default dict -}}
{{- $secret := $m.passwordSecret | default $o.passwordSecret -}}
{{- if not $secret -}}
{{- fail "database.migration.passwordSecret is required: the Secret holding the owner's password, mounted by the migration Job alone" -}}
{{- end -}}
{{- dict "role" ($m.role | default $o.role | default "url_shortener_owner") "passwordSecret" $secret "passwordKey" ($m.passwordKey | default $o.passwordKey | default "password") | toJson -}}
{{- end -}}

{{/*
The prober's own name, and the account it runs as under the transport identity:
`prober.serviceAccount.name`, or `<release>-prober`. A separate account from
every component's, so an allow-list can name it alone.
*/}}
{{- define "url-shortener.proberName" -}}
{{- printf "%s-prober" (include "url-shortener.name" .) -}}
{{- end -}}

{{- define "url-shortener.proberServiceAccountName" -}}
{{- .Values.prober.serviceAccount.name | default (include "url-shortener.proberName" .) -}}
{{- end -}}
