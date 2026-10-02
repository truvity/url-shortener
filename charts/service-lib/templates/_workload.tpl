{{/*
The ServiceAccount a component runs as. The chart GRANTS nothing: it names
the account and carries annotations, and which mechanism binds an account to
rights outside the cluster is the platform's, and differs between clusters in
one estate. `platform.serviceAccount.create: false` is for a platform that
creates the accounts itself; they must then exist, and the Deployment still
names one.
*/}}
{{- define "service-lib.serviceAccount" -}}
{{- $sa := (.platform | default dict).serviceAccount | default dict -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "service-lib.serviceAccountName" . }}
  labels: {{- include "service-lib.componentLabels" . | nindent 4 }}
  {{- with $sa.annotations }}
  annotations: {{- include "service-lib.annotations" . | nindent 4 }}
  {{- end }}
{{- end -}}

{{/* The pod-level security context. `fsGroup` is the one that is easy to omit: a CSI driver writes what it mounts owned by root, and a process running as anyone else cannot read its own certificate. */}}
{{- define "service-lib.podSecurityContext" -}}
{{- $s := (.platform | default dict).podSecurity | default dict -}}
securityContext:
  runAsNonRoot: true
  runAsUser: {{ $s.runAsUser | default 65532 }}
  runAsGroup: {{ $s.runAsGroup | default 65532 }}
  fsGroup: {{ $s.fsGroup | default 65532 }}
  seccompProfile:
    type: RuntimeDefault
{{- end -}}

{{/* The container half of the Pod Security `restricted` profile. */}}
{{- define "service-lib.containerSecurityContext" -}}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
      - ALL
{{- end -}}

{{/*
The drain: ONE number, used three times. The service's own shutdown timeout
is `config.drain.seconds`, the grace period Kubernetes grants is that plus the
pre-stop delay plus a margin, and the pre-stop delay is
`platform.drain.preStopSeconds`. They disagree by default, and both ways of
disagreeing look like a network fault: a grace period shorter than the timeout
kills a draining process, and no pre-stop delay means traffic keeps arriving
for as long as the routing layer takes to notice the endpoint is gone.
*/}}
{{- define "service-lib.preStopSeconds" -}}
{{- $drain := (.platform | default dict).drain | default dict -}}
{{- /* hasKey, not `default`: zero is a legitimate delay, and `default` would turn it into five. */ -}}
{{- ternary $drain.preStopSeconds 5 (hasKey $drain "preStopSeconds") -}}
{{- end -}}

{{- define "service-lib.terminationGrace" -}}
{{- with (.config | default dict).drain -}}
terminationGracePeriodSeconds: {{ add .seconds (include "service-lib.preStopSeconds" $) 5 }}
{{- end -}}
{{- end -}}

{{/*
The Deployment. Everything in it that depends on the process comes from
`config`; everything that depends on the platform from `platform`; and
nothing is taken from anywhere else.

A rollout is gapless (`maxUnavailable: 0`: the replacement is READY before the
incumbent is touched; the default is 25%, which on two replicas means one
goes away first). Replicas are spread across machines with ScheduleAnyway,
not DoNotSchedule: a single-node cluster is the ordinary case for a gate and a
laptop, and a constraint that cannot be met there leaves pods Pending.
*/}}
{{- define "service-lib.deployment" -}}
{{- $p := .platform | default dict -}}
{{- $strategy := $p.strategy | default dict -}}
{{- $fileCfg := $p.config | default dict -}}
{{- $ports := include "service-lib.ports" . -}}
{{- $replicas := ternary $p.replicas 1 (hasKey $p "replicas") -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "service-lib.fullname" . }}
  labels: {{- include "service-lib.labels" .context | nindent 4 }}
spec:
  replicas: {{ $replicas }}
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: {{ ternary $strategy.maxUnavailable 0 (hasKey $strategy "maxUnavailable") }}
      maxSurge: {{ ternary $strategy.maxSurge 1 (hasKey $strategy "maxSurge") }}
  selector:
    matchLabels: {{- include "service-lib.selectorLabels" . | nindent 6 }}
  template:
    metadata:
      labels: {{- include "service-lib.componentLabels" . | nindent 8 }}
      annotations:
        # Restart on a configuration change. Without this a `helm upgrade`
        # that only edits the ConfigMap changes nothing at all: the pods keep
        # the file they started with, and the deploy reads as successful.
        checksum/config: {{ include "service-lib.configChecksum" . }}
    spec:
      serviceAccountName: {{ include "service-lib.serviceAccountName" . }}
      {{- include "service-lib.podSecurityContext" . | nindent 6 }}
      {{- include "service-lib.terminationGrace" . | nindent 6 }}
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels: {{- include "service-lib.selectorLabels" . | nindent 14 }}
      containers:
        - name: {{ .name }}
          image: {{ include "service-lib.image" . }}
          imagePullPolicy: {{ $p.imagePullPolicy | default "IfNotPresent" }}
          {{- include "service-lib.containerSecurityContext" . | nindent 10 }}
          {{- if $fileCfg.pathEnv }}
          {{- /* The path is an environment variable: no argument. */}}
          {{- else }}
          args: [{{ $fileCfg.pathFlag | default "-config" | quote }}, {{ include "service-lib.configPath" . | quote }}]
          {{- end }}
          env:
            {{- include "service-lib.telemetryEnv" . | nindent 12 }}
            {{- if $fileCfg.pathEnv }}
            - name: {{ $fileCfg.pathEnv }}
              value: {{ include "service-lib.configPath" . | quote }}
            {{- end }}
            {{- with $p.env }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
            {{- include "service-lib.secretEnv" . | nindent 12 }}
          ports: {{- include "service-lib.containerPorts" . | nindent 12 }}
          livenessProbe: {{- include "service-lib.probe" (dict "probes" $p.probes "kind" "liveness") | nindent 12 }}
          readinessProbe: {{- include "service-lib.probe" (dict "probes" $p.probes "kind" "readiness") | nindent 12 }}
          {{- if (($p.probes | default dict).startup) }}
          startupProbe: {{- include "service-lib.probe" (dict "probes" $p.probes "kind" "startup") | nindent 12 }}
          {{- end }}
          lifecycle:
            preStop:
              sleep:
                seconds: {{ include "service-lib.preStopSeconds" . }}
          volumeMounts:
            - name: config
              mountPath: {{ include "service-lib.configMountPath" . }}
              readOnly: true
            {{- include "service-lib.identityMount" . | nindent 12 }}
            {{- with $p.volumeMounts }}
            {{- toYaml . | nindent 12 }}
            {{- end }}
          resources: {{- toYaml ($p.resources | default dict) | nindent 12 }}
      volumes:
        - name: config
          configMap:
            name: {{ include "service-lib.fullname" . }}-config
        {{- include "service-lib.identityVolume" . | nindent 8 }}
        {{- with $p.volumes }}
        {{- toYaml . | nindent 8 }}
        {{- end }}
{{- end -}}

{{/*
The Service. Its ports are the listener's, derived from `config.listen` (see
"service-lib.ports"); the probes listener is NOT in it. Readiness must be
answerable when the service's own listener is saturated, which is exactly when
somebody is asking, and a probe endpoint reachable from outside is an
information leak nobody meant to ship.
*/}}
{{- define "service-lib.service" -}}
{{- if include "service-lib.hasService" . -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ include "service-lib.fullname" . }}
  labels: {{- include "service-lib.labels" .context | nindent 4 }}
spec:
  selector: {{- include "service-lib.selectorLabels" . | nindent 4 }}
  ports: {{- include "service-lib.servicePorts" . | nindent 4 }}
{{- end -}}
{{- end -}}

{{/*
A whole component: its ConfigMap, its ServiceAccount (unless the platform
creates the accounts), its Deployment and, if it listens, its Service. For a
chart with one workload this is the entire template.
*/}}
{{- define "service-lib.component" -}}
{{ include "service-lib.configMap" . }}
{{- if ((.platform | default dict).serviceAccount | default dict | dig "create" true) }}
---
{{ include "service-lib.serviceAccount" . }}
{{- end }}
---
{{ include "service-lib.deployment" . }}
{{- with include "service-lib.service" . }}
---
{{ . }}
{{- end }}
{{- end -}}
