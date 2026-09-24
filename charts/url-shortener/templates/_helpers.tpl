{{/*
The install's name, which every object here is prefixed with. Two installs
in one namespace must not collide, and a name derived from the release is
the only thing that guarantees it.
*/}}
{{- define "url-shortener.name" -}}
{{- .Release.Name -}}
{{- end -}}

{{- define "url-shortener.labels" -}}
app.kubernetes.io/name: url-shortener
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
The image reference: one repository per component under a common prefix,
which is what every image builder produces by default and what a registry's
own UI expects.

A digest when there is one, and a tag only when there is not. An image that
can move under a running deployment is one nobody can roll back to, so the
release stamps a digest and this template prefers it.

ONE DIGEST PER COMPONENT. This chart deploys six of them, and they are six
different images — a single `image.digest` for all of them would deploy the
same container six times, each under a name suggesting otherwise. It read
that way until a real cluster was in front of it, because a render is
perfectly happy to repeat a digest and the chart's own tests only validated
the configuration files.
*/}}
{{- define "url-shortener.image" -}}
{{- $i := index .root.Values.images .component -}}
{{- $ref := printf "%s/%s" $i.registry $i.repository -}}
{{- if $i.digest -}}
{{ $ref }}@{{ $i.digest }}
{{- else if $i.tag -}}
{{ $ref }}:{{ $i.tag }}
{{- else if .root.Chart.AppVersion -}}
{{- /*
Nothing stamped, so the chart's own version — which is what a `helm
install` from a checkout gets, and the one thing such a chart knows about
the images built beside it. A PUBLISHED chart never reaches this branch:
its release refuses to package without a digest per entry.
*/ -}}
{{ $ref }}:{{ .root.Chart.AppVersion }}
{{- else -}}
{{ fail (printf "no image for %s: set images.%s.digest, images.%s.tag, or publish this chart with an appVersion" .component .component .component) }}
{{- end -}}
{{- end -}}

{{/*
A database password, as an environment variable read from a Secret.

The chart takes the NAME of a secret, never a value: a chart that generated a
password would put it in the release's own stored manifest, where anyone who
can read a release can read the password.

Takes the role block (.Values.database.owner or .Values.database.app), so the
migration and the services cannot accidentally be handed the same credential.
*/}}
{{- define "url-shortener.passwordEnv" -}}
{{- if ne .auth "certificate" -}}
- name: DATABASE_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ required "passwordSecret is required for a role that authenticates with a password" .passwordSecret }}
      key: {{ .passwordKey | default "password" }}
{{- end }}
{{- end -}}

{{/*
The two account names: what was asked for, or the release's own, suffixed.
*/}}
{{- define "url-shortener.appServiceAccountName" -}}
{{- .Values.serviceAccount.app.name | default (printf "%s-app" (include "url-shortener.name" .)) -}}
{{- end -}}

{{- define "url-shortener.migrateServiceAccountName" -}}
{{- .Values.serviceAccount.migrate.name | default (printf "%s-migrate" (include "url-shortener.name" .)) -}}
{{- end -}}

{{/*
The pod-level settings that make a replacement gapless, shared by every
workload so that one number governs all of them.

terminationGracePeriodSeconds is the WHOLE budget: the pre-stop delay, plus
what the service is given to finish, plus a margin. Kubernetes starts
counting at SIGTERM, so a grace period equal to the service's own timeout
kills it at the exact moment it would have finished.

The pre-stop delay does nothing except take time. That is its job: readiness
fails the moment the pod is marked for deletion, and the routing layer needs
a moment to notice before the process stops accepting. Without it a rollout
drops the requests that were already in flight toward this pod.
*/}}
{{- define "url-shortener.drain" -}}
terminationGracePeriodSeconds: {{ add .Values.drain.seconds .Values.drain.preStopSeconds 5 }}
{{- end -}}

{{- define "url-shortener.preStop" -}}
lifecycle:
  preStop:
    sleep:
      seconds: {{ .Values.drain.preStopSeconds }}
{{- end -}}

{{/*
Spread replicas across machines, or the budget is satisfied by two pods that
die together.

ScheduleAnyway, not DoNotSchedule: a single-node cluster is the ordinary case
for the gate and for a laptop, and a constraint that cannot be met there
leaves pods Pending with an event nobody reads.
*/}}
{{- define "url-shortener.spread" -}}
topologySpreadConstraints:
  - maxSkew: 1
    topologyKey: kubernetes.io/hostname
    whenUnsatisfiable: ScheduleAnyway
    labelSelector:
      matchLabels:
        app.kubernetes.io/name: url-shortener
        app.kubernetes.io/instance: {{ .Release.Name }}
        app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/*
Whether the transport is authenticated at all. Everything TLS-shaped in this
chart is behind this, so that the default render is byte-identical to one
from a chart that had never heard of it — which is what a golden proves.
*/}}
{{- define "url-shortener.tlsOn" -}}
{{- if ne .Values.tls.mode "off" }}yes{{ end -}}
{{- end -}}

{{/*
The volume the platform mounts the identity into.

An ephemeral CSI volume, not a secret: the key lives in the pod and nowhere
else, so a workload that can read secrets in its namespace still cannot read
a neighbour's key. The driver derives the identity from the account this pod
runs as; nothing here names an identity, because a chart that could name one
could name somebody else's.
*/}}
{{- define "url-shortener.identityVolume" -}}
{{- if include "url-shortener.tlsOn" . }}
- name: identity
  csi:
    driver: {{ .Values.tls.csiDriver }}
    readOnly: true
{{- end }}
{{- end -}}

{{- define "url-shortener.identityMount" -}}
{{- if include "url-shortener.tlsOn" . }}
- name: identity
  mountPath: {{ .Values.tls.mountPath }}
  readOnly: true
{{- end }}
{{- end -}}

{{/*
The pod's security context.

`fsGroup` is the one that matters here and the one that is easy to omit. A
CSI driver writes what it mounts owned by root, and a process running as
anyone else cannot read it. The symptom is a permission error on a
certificate authority file, several layers away from anything that mentions
identity, and it appears only once the transport is turned on.
*/}}
{{- define "url-shortener.podSecurity" -}}
securityContext:
  runAsNonRoot: true
  runAsUser: {{ .Values.podSecurity.runAsUser }}
  runAsGroup: {{ .Values.podSecurity.runAsGroup }}
  fsGroup: {{ .Values.podSecurity.fsGroup }}
{{- end -}}

{{/*
Telemetry, as OpenTelemetry's OWN environment variables.

Decision 0006: no service reads telemetry from its configuration file.
The specification defines these variables, every language's SDK reads
them without being asked, and every document about OpenTelemetry is
written in terms of them. A schema naming three of them would be a
ceiling, a second vocabulary, and a precedence question at three in the
morning.

They still belong beside the rest of the service's configuration in this
chart's values, which is what the decision asks of a chart that sets
them — they simply leave as variables rather than as file keys.

NO ENDPOINT MEANS DO NOT EXPORT, and that is done here rather than in six
programs: with no endpoint the exporters are set to `none`, so an SDK
that would otherwise default to localhost and retry forever does nothing
at all. That is the default a laptop needs, and it is why no service
carries an enable flag or an environment-name switch — the failure
decision 0006 records is a service that exported to a console in
production because nothing set the variable the code was testing.

Takes the root context and the component name; `service.name` is the
release and the component, which is stable for the life of the pod and
carries no request, tenant or version in it.
*/}}
{{- define "url-shortener.telemetryEnv" -}}
{{- $otel := .root.Values.otel | default dict -}}
- name: OTEL_SERVICE_NAME
  value: {{ printf "%s-%s" .root.Release.Name .component | quote }}
{{- if $otel.endpoint }}
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ $otel.endpoint | quote }}
- name: OTEL_EXPORTER_OTLP_PROTOCOL
  value: {{ $otel.protocol | default "http/protobuf" | quote }}
- name: OTEL_TRACES_EXPORTER
  value: "otlp"
- name: OTEL_METRICS_EXPORTER
  value: "otlp"
{{- /*
Logs stay on stdout. A node agent already collects every container's
stdout into the same store under the same namespace, so an OTLP log
exporter buys a second copy of what is already there — and a service
whose logs exist ONLY over OTLP loses them exactly when the exporter is
the thing that broke.
*/}}
- name: OTEL_LOGS_EXPORTER
  value: "none"
- name: OTEL_TRACES_SAMPLER
  value: {{ $otel.tracesSampler | default "parentbased_traceidratio" | quote }}
- name: OTEL_TRACES_SAMPLER_ARG
  value: {{ $otel.sampleRatio | default "0.1" | quote }}
{{- with $otel.resourceAttributes }}
{{- /*
Extra resource attributes ride along as pod-level fields. Three of them
become metric labels and no more — the rest land on `target_info` and
nowhere else — so anything to be filtered on must be a METRIC attribute
rather than a resource one.
*/}}
{{- $attrs := . }}
- name: OTEL_RESOURCE_ATTRIBUTES
  value: {{ range $i, $k := (keys $attrs | sortAlpha) }}{{ if $i }},{{ end }}{{ $k }}={{ get $attrs $k }}{{ end }}
{{- end }}
{{- else }}
{{- /* No endpoint: export nothing, rather than to localhost. */}}
- name: OTEL_TRACES_EXPORTER
  value: "none"
- name: OTEL_METRICS_EXPORTER
  value: "none"
- name: OTEL_LOGS_EXPORTER
  value: "none"
{{- end }}
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
The application role's connection string, and how it proves who it is.

With a certificate there is no password anywhere in this path: not in the
file, not in a variable, not in a Secret the pod reads as one. `sslmode`
becomes verify-full rather than require, because a client certificate
proves the CLIENT to the server and does nothing in the other direction
-- require encrypts and verifies nobody, which is the asymmetry that makes
"we use TLS" mean less than people think.
*/}}
{{- define "url-shortener.databaseURL" -}}
{{- $db := .Values.database -}}
{{- if eq $db.app.auth "certificate" -}}
postgres://{{ $db.app.role }}@{{ $db.host }}:5432/{{ $db.name }}?sslmode=verify-full&sslcert=/etc/url-shortener/db/tls.crt&sslkey=/etc/url-shortener/db/tls.key&sslrootcert=/etc/url-shortener/db/ca.crt
{{- else -}}
postgres://{{ $db.app.role }}@{{ $db.host }}:5432/{{ $db.name }}?sslmode=require
{{- end -}}
{{- end }}

{{/*
The certificate, mounted read-only, and only where a role uses one.
*/}}
{{- define "url-shortener.databaseCertVolume" -}}
{{- if eq .Values.database.app.auth "certificate" -}}
- name: database-identity
  secret:
    secretName: {{ required "database.app.certificateSecret is required when database.app.auth is certificate" .Values.database.app.certificateSecret }}
    defaultMode: 0400
{{- end }}
{{- end -}}

{{- define "url-shortener.databaseCertMount" -}}
{{- if eq .Values.database.app.auth "certificate" -}}
- name: database-identity
  mountPath: /etc/url-shortener/db
  readOnly: true
{{- end }}
{{- end -}}
