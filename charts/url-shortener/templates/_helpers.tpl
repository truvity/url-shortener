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
The image reference.

A digest when there is one, and a tag only when there is not. An image that
can move under a running deployment is one nobody can roll back to, so the
release stamps a digest and this template prefers it.
*/}}
{{- define "url-shortener.image" -}}
{{- $i := .Values.image -}}
{{- if $i.digest -}}
{{ $i.repository }}-{{ .component }}@{{ $i.digest }}
{{- else if $i.tag -}}
{{ $i.repository }}-{{ .component }}:{{ $i.tag }}
{{- else -}}
{{ fail "image.digest or image.tag must be set: an image reference with neither is not a deployable thing" }}
{{- end -}}
{{- end -}}

{{/*
The database host. Created by this chart, or supplied.
*/}}
{{- define "url-shortener.dbHost" -}}
{{- if .Values.infra.enabled -}}
{{ .Release.Name }}-pg-rw
{{- else -}}
{{- required "database.host is required when infra.enabled is false: the chart has nothing to derive it from" .Values.database.host -}}
{{- end -}}
{{- end -}}

{{/*
The NATS URL. Created by this chart, or supplied.
*/}}
{{- define "url-shortener.natsURL" -}}
{{- if .Values.infra.enabled -}}
nats://nats.nats.svc:4222
{{- else -}}
{{- required "events.url is required when infra.enabled is false" .Values.events.url -}}
{{- end -}}
{{- end -}}

{{/*
The secret holding the database password, and its key. When this chart
created the database, the operator made the secret; otherwise the values say
where it is.
*/}}
{{- define "url-shortener.passwordSecret" -}}
{{- if .Values.infra.enabled -}}
{{ .Release.Name }}-pg-app
{{- else -}}
{{- required "database.passwordSecret is required when infra.enabled is false" .Values.database.passwordSecret -}}
{{- end -}}
{{- end -}}

{{- define "url-shortener.passwordKey" -}}
{{- if .Values.infra.enabled -}}password{{- else -}}{{ .Values.database.passwordKey }}{{- end -}}
{{- end -}}
