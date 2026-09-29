{{/*
The install's name, which every object here is prefixed with. Two installs
in one namespace must not collide, and a name derived from the release is
the only thing that guarantees it.
*/}}
{{- define "url-shortener-e2e.name" -}}
{{- .Release.Name -}}
{{- end -}}

{{- define "url-shortener-e2e.labels" -}}
app.kubernetes.io/name: url-shortener-e2e
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
The e2e image reference — the SAME shape (and the same fallback order) as
charts/url-shortener's own "url-shortener.image": a digest when there is
one, a tag only when there is not, and the chart's own appVersion for a
render with neither, which is what a `helm install` from a checkout gets.
A published chart always carries a digest — helmctl refuses to package one
that does not (see values.yaml's own comment on the images map).
*/}}
{{- define "url-shortener-e2e.image" -}}
{{- $i := .Values.images.e2e -}}
{{- $ref := printf "%s/%s" $i.registry $i.repository -}}
{{- if $i.digest -}}
{{ $ref }}@{{ $i.digest }}
{{- else if $i.tag -}}
{{ $ref }}:{{ $i.tag }}
{{- else if .Chart.AppVersion -}}
{{ $ref }}:{{ .Chart.AppVersion }}
{{- else -}}
{{ fail "no image for e2e: set images.e2e.digest, images.e2e.tag, or publish this chart with an appVersion" }}
{{- end -}}
{{- end -}}

{{/*
This Job's own account name: what was asked for, or the release's own,
suffixed.
*/}}
{{- define "url-shortener-e2e.serviceAccountName" -}}
{{- .Values.serviceAccount.name | default (printf "%s-e2e" (include "url-shortener-e2e.name" .)) -}}
{{- end -}}

{{/*
The Job's own name — folding in THIS CHART'S OWN VERSION, not the release
name alone.

A Job's spec is immutable, so re-applying this chart at a new version
under the SAME Job name would not converge, it would be REFUSED outright —
by `helm upgrade` on an ordinary install, and by a GitOps controller's
`kubectl apply` just the same, since neither one deletes and recreates an
object that already exists under a name it still owns. Folding the version
in sidesteps that entirely: each release of this chart names a Job nothing
before it ever created, so applying it is always a create, never a patch
that Kubernetes has to refuse. The Job this replaces is left for
job.ttlSecondsAfterFinished to remove once it finishes, exactly like every
other version before it.

There are no `helm.sh/hook` annotations anywhere in this chart for the
same reason `templates/verification.yaml`'s hook Job needs them: THAT
Job's identity is "the ONE verification of this release", re-created on
every install and upgrade under one hook-managed name. This chart's Job is
its own release, and its identity is "this chart's version" — the name
already carries that, so nothing here needs Helm's own hook machinery to
say so a second way.

Truncated to 63: Kubernetes stamps this name onto the Pod template's own
`job-name` LABEL, and a label's VALUE — unlike a resource name — cannot
exceed that.
*/}}
{{- define "url-shortener-e2e.jobName" -}}
{{- $v := regexReplaceAll "[^a-z0-9]+" (.Chart.Version | lower) "-" | trimSuffix "-" -}}
{{- printf "%s-%s" (include "url-shortener-e2e.name" .) $v | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
The pod's security context — the same shape as
charts/url-shortener's own "url-shortener.podSecurity", and the same
default values (values.yaml's podSecurity), so a platform that already
trusts those trusts this Job too.
*/}}
{{- define "url-shortener-e2e.podSecurity" -}}
securityContext:
  runAsNonRoot: true
  runAsUser: {{ .Values.podSecurity.runAsUser }}
  runAsGroup: {{ .Values.podSecurity.runAsGroup }}
  fsGroup: {{ .Values.podSecurity.fsGroup }}
{{- end -}}

{{/*
The token this Job exchanges at traces.tokenExchange.tokenURL for a bearer
token scoped to traces.tokenExchange.client — the SAME projected-token
shape as charts/url-shortener's own "url-shortener.eventsTokenVolume": a
token this Job cannot forge, which expires on its own, never a secret this
chart or its values ever hold.

Rendered only when traces.tokenExchange.tokenURL is set — an empty
tokenURL means traces.url (if set at all) admits anonymous readers, and
there is nothing here to project a token for.
*/}}
{{- define "url-shortener-e2e.tracesTokenVolume" -}}
{{- $te := .Values.traces.tokenExchange | default dict -}}
{{- if $te.tokenURL -}}
- name: traces-token
  projected:
    sources:
      - serviceAccountToken:
          audience: {{ $te.audience | default "access-issuer" | quote }}
          expirationSeconds: {{ $te.expirationSeconds | default 3600 }}
          path: token
{{- end -}}
{{- end -}}

{{- define "url-shortener-e2e.tracesTokenMount" -}}
{{- $te := .Values.traces.tokenExchange | default dict -}}
{{- if $te.tokenURL -}}
- name: traces-token
  mountPath: /var/run/traces
  readOnly: true
{{- end -}}
{{- end -}}

{{/*
The CA bundle traces.url is verified against, when its leaf is not signed
by the suite image's own default trust store — see
examples/url-shortener/e2e/traceauth's own package doc comment for why
this is NEVER the trust store traces.tokenExchange.tokenURL is verified
against. Rendered only when traces.caConfigMap is set.
*/}}
{{- define "url-shortener-e2e.tracesCAVolume" -}}
{{- with .Values.traces.caConfigMap -}}
- name: traces-ca
  configMap:
    name: {{ . }}
{{- end -}}
{{- end -}}

{{- define "url-shortener-e2e.tracesCAMount" -}}
{{- with .Values.traces.caConfigMap -}}
- name: traces-ca
  mountPath: /var/run/traces-ca
  readOnly: true
{{- end -}}
{{- end -}}

{{/*
The prober's own name, and its security context — read from
.Values.prober.podSecurity rather than .Values.podSecurity above, because
the prober is a SEPARATE workload from the Job the rest of this file is
about, with its own settings rather than a share of the Job's.
*/}}
{{- define "url-shortener-e2e.proberName" -}}
{{- printf "%s-prober" (include "url-shortener-e2e.name" .) -}}
{{- end -}}

{{/*
Whether this chart's own client-side transport identity is on at all — the
SAME shape charts/url-shortener's own "url-shortener.tlsOn" states, so a
chart installed with it off renders byte-identical to one that has never
heard of it. See values.yaml's own comment on the top-level `tls` block for
why this is a value this chart carries independently, not a copy of the
application release's.
*/}}
{{- define "url-shortener-e2e.tlsOn" -}}
{{- if ne .Values.tls.mode "off" }}yes{{ end -}}
{{- end -}}

{{/*
The volume the platform mounts the prober's identity into, and where it is
mounted — the SAME ephemeral CSI volume shape charts/url-shortener's own
"url-shortener.identityVolume" / "...identityMount" use, reused here rather
than forked: not a Secret, so a workload that can read Secrets in its
namespace still cannot read a neighbour's key. The driver derives the
identity from the account the POD runs as
("url-shortener-e2e.proberServiceAccountName" below) — nothing here names
one, because a chart that could name one could name somebody else's.
*/}}
{{- define "url-shortener-e2e.identityVolume" -}}
{{- if include "url-shortener-e2e.tlsOn" . }}
- name: identity
  csi:
    driver: {{ .Values.tls.csiDriver }}
    readOnly: true
{{- end }}
{{- end -}}

{{- define "url-shortener-e2e.identityMount" -}}
{{- if include "url-shortener-e2e.tlsOn" . }}
- name: identity
  mountPath: {{ .Values.tls.mountPath }}
  readOnly: true
{{- end }}
{{- end -}}

{{/*
The prober's own account name — a SEPARATE account from the e2e Job's
("url-shortener-e2e.serviceAccountName" above), because the two carry
different grants: the Job's account is what the Kubernetes API RBAC in
templates/rbac.yaml binds to, and this one is what a CSI-mounted identity,
and — once a platform grants it — the application release's own
`tls.peers`, name. Empty means the release's own, suffixed.
*/}}
{{- define "url-shortener-e2e.proberServiceAccountName" -}}
{{- .Values.prober.serviceAccount.name | default (printf "%s-prober" (include "url-shortener-e2e.name" .)) -}}
{{- end -}}

{{- define "url-shortener-e2e.proberPodSecurity" -}}
securityContext:
  runAsNonRoot: true
  runAsUser: {{ .Values.prober.podSecurity.runAsUser }}
  runAsGroup: {{ .Values.prober.podSecurity.runAsGroup }}
  fsGroup: {{ .Values.prober.podSecurity.fsGroup }}
{{- end -}}

{{/*
The prober image reference — the same shape (and the same fallback order)
as "url-shortener-e2e.image" above, for the SECOND image this chart
carries. Read only when .Values.prober.enabled is true (templates/
prober.yaml), so a chart installed with the prober left off never has to
name a digest for an image it never runs.
*/}}
{{- define "url-shortener-e2e.proberImage" -}}
{{- $i := .Values.images.prober -}}
{{- $ref := printf "%s/%s" $i.registry $i.repository -}}
{{- if $i.digest -}}
{{ $ref }}@{{ $i.digest }}
{{- else if $i.tag -}}
{{ $ref }}:{{ $i.tag }}
{{- else if .Chart.AppVersion -}}
{{ $ref }}:{{ .Chart.AppVersion }}
{{- else -}}
{{ fail "no image for prober: set images.prober.digest, images.prober.tag, or publish this chart with an appVersion" }}
{{- end -}}
{{- end -}}

{{/*
Telemetry for the prober, as OpenTelemetry's own environment variables —
the SAME rule charts/url-shortener's own "url-shortener.telemetryEnv"
states in full (decision 0006): no endpoint means the exporters are
"none", set here rather than decided by the binary, so nothing in this
chart carries an enable flag.

Only the prober reads this. The e2e Job is a one-shot test run that
reports its result as a Job condition and its own log, not a workload a
bake window watches over time, so it carries none of this.
*/}}
{{- define "url-shortener-e2e.telemetryEnv" -}}
{{- $otel := .Values.otel | default dict -}}
- name: OTEL_SERVICE_NAME
  value: {{ include "url-shortener-e2e.proberName" . | quote }}
{{- if $otel.endpoint }}
- name: OTEL_EXPORTER_OTLP_ENDPOINT
  value: {{ $otel.endpoint | quote }}
- name: OTEL_EXPORTER_OTLP_PROTOCOL
  value: {{ $otel.protocol | default "http/protobuf" | quote }}
- name: OTEL_TRACES_EXPORTER
  value: "otlp"
- name: OTEL_METRICS_EXPORTER
  value: "otlp"
{{- /* Logs stay on stdout — see the app chart's identical comment. */}}
- name: OTEL_LOGS_EXPORTER
  value: "none"
- name: OTEL_TRACES_SAMPLER
  value: {{ $otel.tracesSampler | default "parentbased_traceidratio" | quote }}
- name: OTEL_TRACES_SAMPLER_ARG
  value: {{ $otel.sampleRatio | default "0.1" | quote }}
{{- with $otel.resourceAttributes }}
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
