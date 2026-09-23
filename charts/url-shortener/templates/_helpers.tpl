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
