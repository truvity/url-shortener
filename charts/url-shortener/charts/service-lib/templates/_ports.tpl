{{/*
The port in a `host:port` address, as the number. Refused when there is none:
the schema's `pattern` already guarantees one for a validated file, and this
is the same rule for a spec that was never validated.
*/}}
{{- define "service-lib.portOf" -}}
{{- $p := regexFind "[0-9]+$" (toString .) -}}
{{- if not $p -}}
{{- fail (printf "%q is not a host:port address" (toString .)) -}}
{{- end -}}
{{- $p -}}
{{- end -}}

{{/*
The ports a component's process binds, as JSON, DERIVED from its own
configuration file and from nothing else:

    config.listen.address  -> http
    config.probes.address  -> probes
    config.tls.address     -> https, only under tls.mode permissive

A chart that wrote a port beside the file would have two numbers for one
fact, and the pod would be reachable on one of them while the process listens
on the other. Under `strict` there is no second port: the listener is the
same one, speaking TLS, so nothing moves.

A component with no `probes` is refused: every workload owes the platform
somewhere to ask whether it is alive (service.md).
*/}}
{{- define "service-lib.ports" -}}
{{- $c := .config | default dict -}}
{{- $out := dict -}}
{{- with $c.listen -}}
{{- $_ := set $out "http" (include "service-lib.portOf" .address | int) -}}
{{- end -}}
{{- if not $c.probes -}}
{{- fail (printf "component %s: config.probes is required: a workload with nowhere to ask whether it is alive cannot be rolled out safely" .name) -}}
{{- end -}}
{{- $_ := set $out "probes" (include "service-lib.portOf" $c.probes.address | int) -}}
{{- if eq (include "service-lib.tlsMode" .) "permissive" -}}
{{- if not $c.tls.address -}}
{{- fail (printf "component %s: config.tls.mode is permissive, so config.tls.address (the authenticated listener) is required" .name) -}}
{{- end -}}
{{- $_ := set $out "https" (include "service-lib.portOf" $c.tls.address | int) -}}
{{- end -}}
{{- toJson $out -}}
{{- end -}}

{{/* Container ports, in the order http, probes, https. */}}
{{- define "service-lib.containerPorts" -}}
{{- $ports := include "service-lib.ports" . | fromJson -}}
{{- range $n := list "http" "probes" "https" -}}
{{- if hasKey $ports $n }}
- name: {{ $n }}
  containerPort: {{ int (get $ports $n) }}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* Whether a component has a Service: it listens, and the platform did not say otherwise. */}}
{{- define "service-lib.hasService" -}}
{{- $p := .platform | default dict -}}
{{- $svc := $p.service | default dict -}}
{{- $listens := ternary "yes" "" (not (not ((.config | default dict).listen))) -}}
{{- if hasKey $svc "enabled" -}}
{{- if $svc.enabled -}}{{- if not $listens -}}{{- fail (printf "component %s: platform.service.enabled but config.listen is absent: a Service for a process that listens on nothing routes to nothing" .name) -}}{{- end -}}yes{{- end -}}
{{- else -}}
{{- $listens -}}
{{- end -}}
{{- end -}}

{{/* Service ports: http on the listen port, https beside it under permissive. */}}
{{- define "service-lib.servicePorts" -}}
{{- $ports := include "service-lib.ports" . | fromJson -}}
{{- range $n := list "http" "https" -}}
{{- if hasKey $ports $n }}
- name: {{ $n }}
  port: {{ int (get $ports $n) }}
  targetPort: {{ $n }}
{{- end -}}
{{- end -}}
{{- end -}}
