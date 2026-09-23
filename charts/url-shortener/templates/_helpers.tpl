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
*/}}
{{- define "url-shortener.image" -}}
{{- $i := .Values.image -}}
{{- if $i.digest -}}
{{ $i.repository }}/{{ .component }}@{{ $i.digest }}
{{- else if $i.tag -}}
{{ $i.repository }}/{{ .component }}:{{ $i.tag }}
{{- else -}}
{{ fail "image.digest or image.tag must be set: an image reference with neither is not a deployable thing" }}
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
- name: DATABASE_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ .passwordSecret }}
      key: {{ .passwordKey | default "password" }}
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
