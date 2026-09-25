{{/*
The runtime role's password secret, resolved once so the two places that
need the name -- the managed role and the ExternalSecret -- cannot drift
apart.

An explicit name wins even when generate is also true, because an install
told exactly what to use should not have the chart go asking a generator
for a second answer nobody will read.
*/}}
{{- define "url-shortener-infra.runtimePasswordSecret" -}}
{{- if .Values.postgres.runtimePasswordSecret -}}
{{ .Values.postgres.runtimePasswordSecret }}
{{- else if .Values.postgres.runtimePassword.generate -}}
{{ .Release.Name }}-pg-runtime
{{- else -}}
{{ required "postgres.runtimePasswordSecret is the NAME of a basic-auth secret, or set postgres.runtimePassword.generate: true; the chart does not invent a password on its own" .Values.postgres.runtimePasswordSecret }}
{{- end -}}
{{- end }}
