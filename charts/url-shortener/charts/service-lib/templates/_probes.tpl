{{/*
One HTTP probe, on the probes listener. The port is the NAME `probes`, which
the container declares from `config.probes.address`, so a probe cannot point
at a port the process is not listening on.

`kind` is liveness, readiness or startup. Liveness is nothing but the
process: a probe that checks a dependency restarts a healthy process and
makes an outage worse. Startup exists for a process that takes a while to
come up, and is off unless the platform asks for it.
*/}}
{{- define "service-lib.probe" -}}
{{- $d := dict "liveness" (dict "path" "/health/live" "periodSeconds" 10) "readiness" (dict "path" "/health/ready" "periodSeconds" 5) "startup" (dict "path" "/health/ready" "periodSeconds" 5) -}}
{{- $own := get (.probes | default dict) .kind | default dict -}}
{{- $base := get $d .kind -}}
{{- $probe := dict "httpGet" (dict "path" ($own.path | default $base.path) "port" "probes") "periodSeconds" ($own.periodSeconds | default $base.periodSeconds) -}}
{{- range $k := list "initialDelaySeconds" "timeoutSeconds" "successThreshold" "failureThreshold" -}}
{{- if hasKey $own $k -}}
{{- $_ := set $probe $k (get $own $k) -}}
{{- end -}}
{{- end -}}
{{- toYaml $probe -}}
{{- end -}}
