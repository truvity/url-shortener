{{/*
Everything in this library takes ONE argument, a component spec:

    (dict "context" $ "name" "<component>" "platform" <dict> "config" <dict>)

`context` is the including chart's root context (so `.Release` and `.Chart`
are the including chart's, never this library's), `name` is the component,
`platform` is what the platform schema describes (schemas/fragments/
platform.json) and `config` is the component's configuration file, exactly
as its own schema describes it. The library reads `config` and never writes
it: what the binary reads is what the chart was given.
*/}}

{{/* <release>-<component>: the name of every object a component owns. */}}
{{- define "service-lib.fullname" -}}
{{- printf "%s-%s" .context.Release.Name .name -}}
{{- end -}}

{{/*
Labels on every object, from the including chart's root context. The version
is the chart's own appVersion, quoted: one that looks like a number must
still be a label VALUE.

Never in a selector: a selector that included it would stop matching the
incumbent replicas the moment a rollout changed it.
*/}}
{{- define "service-lib.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end -}}

{{/* Labels plus the component, for the objects that belong to ONE component. */}}
{{- define "service-lib.componentLabels" -}}
{{ include "service-lib.labels" .context }}
app.kubernetes.io/component: {{ .name }}
{{- end -}}

{{/* The selector: name, instance and component, and nothing that changes. */}}
{{- define "service-lib.selectorLabels" -}}
app.kubernetes.io/name: {{ .context.Chart.Name }}
app.kubernetes.io/instance: {{ .context.Release.Name }}
app.kubernetes.io/component: {{ .name }}
{{- end -}}

{{/*
The account a component runs as. EVERY component has its own, always: a
workload identity is namespace plus account, so a shared one makes two
components indistinguishable to an allow-list. `default` is refused for the
same reason, and because it is the account every other workload without one
shares.
*/}}
{{- define "service-lib.serviceAccountName" -}}
{{- $sa := (.platform | default dict).serviceAccount | default dict -}}
{{- $name := $sa.name | default (include "service-lib.fullname" .) -}}
{{- if eq $name "default" -}}
{{- fail (printf "component %s: serviceAccount.name \"default\" is refused: every component runs as its own account, and \"default\" is the one every workload without a choice shares" .name) -}}
{{- end -}}
{{- $name -}}
{{- end -}}

{{/*
The image reference: the platform's `image` block, or the including chart's
top-level `images.<component>` (the map a release stamps and refuses to
publish with an empty digest).

A digest when there is one and a tag only when there is not: an image that
can move under a running deployment is one nobody can roll back to. Nothing
stamped falls back to the chart's own appVersion, which is what a `helm
install` from a checkout gets; a published chart never reaches that branch.
*/}}
{{- define "service-lib.image" -}}
{{- $p := .platform | default dict -}}
{{- $images := .context.Values.images | default dict -}}
{{- $i := $p.image | default (get $images .name) | default dict -}}
{{- if not $i.repository -}}
{{- fail (printf "component %s: no image: set platform.image or images.%s with a repository" .name .name) -}}
{{- end -}}
{{- $ref := $i.repository -}}
{{- if $i.registry -}}
{{- $ref = printf "%s/%s" $i.registry $i.repository -}}
{{- end -}}
{{- if $i.digest -}}
{{ $ref }}@{{ $i.digest }}
{{- else if $i.tag -}}
{{ $ref }}:{{ $i.tag }}
{{- else if .context.Chart.AppVersion -}}
{{ $ref }}:{{ .context.Chart.AppVersion }}
{{- else -}}
{{- fail (printf "no image for %s: set its digest or tag, or publish this chart with an appVersion" .name) -}}
{{- end -}}
{{- end -}}

{{/*
A map of annotations, every key and value QUOTED. An annotation's value is a
string whatever it looks like (`"true"`, `"10"`), and a key such as
`helm.sh/hook` reads unambiguously quoted; the quoting is what a reader and a
text search both rely on.
*/}}
{{- define "service-lib.annotations" -}}
{{- $lines := list -}}
{{- range $k, $v := . -}}
{{- $lines = append $lines (printf "%s: %s" (quote $k) (quote $v)) -}}
{{- end -}}
{{- join "\n" $lines -}}
{{- end -}}
