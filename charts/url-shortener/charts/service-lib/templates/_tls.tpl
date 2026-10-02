{{/*
The transport mode a component's own configuration asks for: `off` when it
has no `tls` block at all.
*/}}
{{- define "service-lib.tlsMode" -}}
{{- ((.config | default dict).tls | default dict).mode | default "off" -}}
{{- end -}}

{{/*
Whether the platform's workload identity is mounted into this component: when
its configuration turns the transport on (`config.tls.mode` permissive or
strict), or when the platform says so (`platform.tls.mount: true`) — which is
what a chart sets for a process that presents no identity of its own but
whose release does.

The identity is an ephemeral CSI volume, not a secret: the key lives in the
pod and nowhere else, so a workload that can read secrets in its namespace
still cannot read a neighbour's key. The driver derives the identity from the
account the pod runs as; nothing here names one.
*/}}
{{- define "service-lib.identityOn" -}}
{{- $tls := (.platform | default dict).tls | default dict -}}
{{- if hasKey $tls "mount" -}}
{{- if $tls.mount -}}yes{{- end -}}
{{- else if ne (include "service-lib.tlsMode" .) "off" -}}yes
{{- end -}}
{{- end -}}

{{/*
Where the identity is mounted, and the check that the file agrees: every file
the configuration names (certFile, keyFile, caFile) must be under the mount
path. A configuration pointing at /a while the volume is at /b is a process
that cannot read its own certificate, which surfaces as a permission error on
an authority file a long way from anything that says identity.
*/}}
{{- define "service-lib.identityPath" -}}
{{- $tls := (.platform | default dict).tls | default dict -}}
{{- $path := $tls.mountPath | default "/var/run/identity" -}}
{{- $cfg := (.config | default dict).tls | default dict -}}
{{- if ne (include "service-lib.tlsMode" .) "off" -}}
{{- range $f := list "certFile" "keyFile" "caFile" -}}
{{- $v := get $cfg $f | default "" -}}
{{- if and $v (not (hasPrefix (printf "%s/" $path) $v)) -}}
{{- fail (printf "component %s: config.tls.%s is %q, which is not under platform.tls.mountPath %q: the file would be read from a place nothing mounts" $.name $f $v $path) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $path -}}
{{- end -}}

{{- define "service-lib.identityVolume" -}}
{{- if include "service-lib.identityOn" . }}
{{- $tls := (.platform | default dict).tls | default dict }}
- name: identity
  csi:
    driver: {{ required (printf "component %s: platform.tls.csiDriver is required once the identity is mounted: it is the platform's, and a default would name one estate's" .name) $tls.csiDriver }}
    readOnly: true
{{- end }}
{{- end -}}

{{- define "service-lib.identityMount" -}}
{{- if include "service-lib.identityOn" . }}
- name: identity
  mountPath: {{ include "service-lib.identityPath" . }}
  readOnly: true
{{- end }}
{{- end -}}
