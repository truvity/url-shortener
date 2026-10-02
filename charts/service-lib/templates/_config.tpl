{{/*
THE CONFIGURATION FILE, VERBATIM: `toYaml` of the component's `config` and
nothing added, removed, renamed or defaulted. What the chart was given is
what the binary reads, and the schema that validated the one validates the
other (decision 0009). A key this library rendered that the binary does not
know would be exactly the drift the configuration contract exists to stop, so
there is no code here that could.

The file's name is `<component>.yaml` unless `platform.config.fileName` says
otherwise, and it is mounted as a DIRECTORY (never a subPath, which a rotation
of the ConfigMap would not reach).
*/}}
{{- define "service-lib.configYaml" -}}
{{- toYaml (.config | default dict) -}}
{{- end -}}

{{- define "service-lib.configFileName" -}}
{{- ((.platform | default dict).config | default dict).fileName | default (printf "%s.yaml" .name) -}}
{{- end -}}

{{- define "service-lib.configMountPath" -}}
{{- ((.platform | default dict).config | default dict).mountPath | default (printf "/etc/%s" .context.Chart.Name) -}}
{{- end -}}

{{/*
The ConfigMap holding the file: one per component, so a pod mounts its own
file and no other, and a change to one component's configuration restarts that
component alone.

`platform.configMap.annotations` exists for a ConfigMap that must be a hook
resource (a pre-install job cannot mount one the release has not created yet).
*/}}
{{- define "service-lib.configMap" -}}
{{- $cm := (.platform | default dict).configMap | default dict -}}
apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ include "service-lib.fullname" . }}-config
  labels: {{- include "service-lib.componentLabels" . | nindent 4 }}
  {{- with $cm.annotations }}
  annotations: {{- include "service-lib.annotations" . | nindent 4 }}
  {{- end }}
data:
  {{ include "service-lib.configFileName" . }}: |
    {{- include "service-lib.configYaml" . | nindent 4 }}
{{- end -}}

{{/*
How the process is told where the file is: ONE argument (`-config <path>`) or,
when `platform.config.pathEnv` names a variable, that variable instead. Never
both and never another input (decision 0002).
*/}}
{{- define "service-lib.configPath" -}}
{{- printf "%s/%s" (include "service-lib.configMountPath" .) (include "service-lib.configFileName" .) -}}
{{- end -}}

{{- define "service-lib.configChecksum" -}}
{{- include "service-lib.configYaml" . | sha256sum -}}
{{- end -}}
