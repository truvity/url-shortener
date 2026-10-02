{{/*
Telemetry, as OpenTelemetry's OWN environment variables (decision 0006). No
service reads telemetry from its configuration file: the specification
defines these variables, and every language's SDK reads them without being
asked.

NO ENDPOINT MEANS DO NOT EXPORT, and that is done here rather than in every
program: with no endpoint the exporters are set to `none`, so an SDK that
would otherwise default to localhost and retry forever does nothing at all.
That is why no service carries an enable flag or an environment-name switch.

`service.name` is the release and the component, which is stable for the
life of the pod and carries no request, tenant or version in it.
*/}}
{{- define "service-lib.telemetryEnv" -}}
{{- $otel := (.platform | default dict).telemetry | default dict -}}
- name: OTEL_SERVICE_NAME
  value: {{ $otel.serviceName | default (include "service-lib.fullname" .) | quote }}
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
Logs stay on stderr. A node agent already collects every container's output
into the same store, so an OTLP log exporter buys a second copy of what is
already there, and a service whose logs exist ONLY over OTLP loses them
exactly when the exporter is the thing that broke.
*/}}
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

{{/*
The environment variables that carry SECRETS, and nothing else (decision
0002): each one declared in `platform.secrets` as the variable's name and the
Secret and key it comes from. The value never appears in this chart's values
or in anything it renders; the configuration file names the VARIABLE (a
`credentialsEnv` key), and the pod's own environment holds the value.

Refused: a declaration with no Secret or no key, which would render a
reference to nothing and fail in the pod, not in the render.
*/}}
{{- define "service-lib.secretEnv" -}}
{{- $secrets := (.platform | default dict).secrets | default dict -}}
{{- range $env := keys $secrets | sortAlpha -}}
{{- $s := get $secrets $env -}}
{{- if not (and $s.secretName $s.key) }}
{{- fail (printf "component %s: platform.secrets.%s needs both secretName and key" $.name $env) }}
{{- end }}
- name: {{ $env }}
  valueFrom:
    secretKeyRef:
      name: {{ $s.secretName }}
      key: {{ $s.key }}
{{- end -}}
{{- end -}}
