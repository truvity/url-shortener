{{/*
The name THIS install is known by, across both charts of the pair.

Two releases share one identity: this chart provisions the stream (and the
database), the application chart connects to it, and both have to agree on
what it is called without either naming the other's release. Defaulting to
this chart's OWN release name is what makes a standalone install work with
nothing set — the common case under platform.md rule 11, where a platform
installs both charts under one release name. A platform that must give the
two different release names sets this explicitly, to the same value it
gives the application release.

REFUSED, not sanitised, when it is not a safe shape. `.Release.Namespace`
and `.Release.Name` are already constrained to a DNS-1123 label by
Kubernetes and Helm; this is a plain string a caller can set to anything,
and it is folded into a NATS stream and subject below, where a literal `.`
would silently create an extra subject token and a space would be refused
by the broker. A name this chart trusts has to be turned away when it is
not the shape trusted — turning it into something safe instead would mean
two platforms spelling the same install differently and getting away with
it until the day their streams collide.
*/}}
{{- define "url-shortener-infra.installName" -}}
{{- $name := .Values.installName | default .Release.Name -}}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]{0,38}[a-z0-9])?$" $name) -}}
{{- fail (printf "installName %q must be a lowercase name of letters, digits and hyphens, at most 40 characters: it is folded into a NATS stream and subject, which is why this refuses it instead of lower-casing or truncating it for you" $name) -}}
{{- end -}}
{{- $name -}}
{{- end -}}

{{/*
The tenant scope every cluster-global name below derives from: this
NAMESPACE and this INSTALL, together.

Neither alone is enough, and each misses a different shape of collision.
Namespace alone collides every install sharing a namespace — two CI runs
against one namespace, each installing under the application's own default
release name. Install name alone collides two installs that happen to
agree on a name in different namespaces — two engineers who each call
their own copy "url-shortener". The pair is the smallest thing that
separates both, because a JetStream stream lives on the broker rather than
inside either namespace: nothing about how Kubernetes scopes its own
objects protects it.

MUST MATCH url-shortener/templates/_helpers.tpl's
"url-shortener.eventsScope". The application chart CONNECTS to the stream
this chart CREATES (platform.md rule 6, "found, not made"), so the two
computing one name from one formula is what keeps them equal without a
value passed between them — platform.md's naming rule is why a value is
not the fix here: the name is the project's own convention, not a
platform's, and a chart that took it as an input could only be installed
correctly by a platform that already agreed with this one's spelling.

Both inputs are already bounded — a namespace to 63 characters by
Kubernetes, this chart's own installName to 40 above — so the
concatenation stays comfortably inside what a NATS stream or subject name
may carry, and is left as one readable token rather than hashed.
*/}}
{{- define "url-shortener-infra.eventsScope" -}}
{{- printf "%s-%s" .Release.Namespace (include "url-shortener-infra.installName" .) -}}
{{- end -}}

{{/*
The JetStream stream's OWN name.

Not the Stream custom resource's Kubernetes object name in infra.yaml,
which Kubernetes already namespaces on its own — this is the name the
broker itself knows the stream by, and the broker has never heard of a
Kubernetes namespace.
*/}}
{{- define "url-shortener-infra.eventsStream" -}}
{{- printf "%s-events" (include "url-shortener-infra.eventsScope" .) -}}
{{- end -}}

{{/*
The two subjects this install's stream carries. Each subject's leading
token is this install's scope, so two installs' subjects never overlap —
even where the broker puts every stream on one shared account and would
otherwise see them all at once.
*/}}
{{- define "url-shortener-infra.redirectSubject" -}}
{{- printf "%s.redirect" (include "url-shortener-infra.eventsScope" .) -}}
{{- end -}}

{{- define "url-shortener-infra.requestSubject" -}}
{{- printf "%s.log" (include "url-shortener-infra.eventsScope" .) -}}
{{- end -}}
