{{/*
The install's name, which every object here is prefixed with. Two installs
in one namespace must not collide, and a name derived from the release is
the only thing that guarantees it.
*/}}
{{- define "url-shortener-e2e.name" -}}
{{- .Release.Name -}}
{{- end -}}

{{- define "url-shortener-e2e.labels" -}}
app.kubernetes.io/name: url-shortener-e2e
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
The e2e image reference — the SAME shape (and the same fallback order) as
charts/url-shortener's own "url-shortener.image": a digest when there is
one, a tag only when there is not, and the chart's own appVersion for a
render with neither, which is what a `helm install` from a checkout gets.
A published chart always carries a digest — helmctl refuses to package one
that does not (see values.yaml's own comment on the images map).
*/}}
{{- define "url-shortener-e2e.image" -}}
{{- $i := .Values.images.e2e -}}
{{- $ref := printf "%s/%s" $i.registry $i.repository -}}
{{- if $i.digest -}}
{{ $ref }}@{{ $i.digest }}
{{- else if $i.tag -}}
{{ $ref }}:{{ $i.tag }}
{{- else if .Chart.AppVersion -}}
{{ $ref }}:{{ .Chart.AppVersion }}
{{- else -}}
{{ fail "no image for e2e: set images.e2e.digest, images.e2e.tag, or publish this chart with an appVersion" }}
{{- end -}}
{{- end -}}

{{/*
This Job's own account name: what was asked for, or the release's own,
suffixed.
*/}}
{{- define "url-shortener-e2e.serviceAccountName" -}}
{{- .Values.serviceAccount.name | default (printf "%s-e2e" (include "url-shortener-e2e.name" .)) -}}
{{- end -}}

{{/*
The Job's own name — folding in THIS CHART'S OWN VERSION, not the release
name alone.

A Job's spec is immutable, so re-applying this chart at a new version
under the SAME Job name would not converge, it would be REFUSED outright —
by `helm upgrade` on an ordinary install, and by a GitOps controller's
`kubectl apply` just the same, since neither one deletes and recreates an
object that already exists under a name it still owns. Folding the version
in sidesteps that entirely: each release of this chart names a Job nothing
before it ever created, so applying it is always a create, never a patch
that Kubernetes has to refuse. The Job this replaces is left for
job.ttlSecondsAfterFinished to remove once it finishes, exactly like every
other version before it.

There are no `helm.sh/hook` annotations anywhere in this chart for the
same reason `templates/verification.yaml`'s hook Job needs them: THAT
Job's identity is "the ONE verification of this release", re-created on
every install and upgrade under one hook-managed name. This chart's Job is
its own release, and its identity is "this chart's version" — the name
already carries that, so nothing here needs Helm's own hook machinery to
say so a second way.

Truncated to 63: Kubernetes stamps this name onto the Pod template's own
`job-name` LABEL, and a label's VALUE — unlike a resource name — cannot
exceed that.
*/}}
{{- define "url-shortener-e2e.jobName" -}}
{{- $v := regexReplaceAll "[^a-z0-9]+" (.Chart.Version | lower) "-" | trimSuffix "-" -}}
{{- printf "%s-%s" (include "url-shortener-e2e.name" .) $v | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
The pod's security context — the same shape as
charts/url-shortener's own "url-shortener.podSecurity", and the same
default values (values.yaml's podSecurity), so a platform that already
trusts those trusts this Job too.
*/}}
{{- define "url-shortener-e2e.podSecurity" -}}
securityContext:
  runAsNonRoot: true
  runAsUser: {{ .Values.podSecurity.runAsUser }}
  runAsGroup: {{ .Values.podSecurity.runAsGroup }}
  fsGroup: {{ .Values.podSecurity.fsGroup }}
{{- end -}}
