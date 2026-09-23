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
