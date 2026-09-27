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

{{/*
The tenant-scoped Postgres identifier the database name and both role names
below build on.

Postgres is the OTHER place this pair's tenant scope has to travel: the
local cluster's single Postgres server is shared by every install the same
way its broker is, so a database or role named the same by two tenants
finds the other's, exactly like an unscoped stream would — see
"url-shortener-infra.eventsScope" above and `docs/guides/testing.md`'s "The
box installs no infra chart" for the collision this closes. A `primary`
install's own CNPG Cluster is not shared with anything, so this costs it
nothing; it is the `test` tier, standing in for that Cluster on a box with
one Postgres for everyone, that needs it.

The scope is folded into the shape Postgres accepts for an unquoted
identifier: lower-cased (it already is, being built from a namespace and an
installName Kubernetes and this chart already constrain to lowercase), `-`
replaced by `_` since Postgres has no hyphen there, anything else stripped,
and started with a letter if it is not already — Postgres refuses a bare
digit at the front even though Kubernetes allows one to start a namespace.

Sized to 57 bytes rather than Postgres' own 63, to leave room for the
LONGER of the two role suffixes below (`_owner`, 6 bytes) — so the database
name and both role names come from ONE shared base and none of them
individually risks the limit the others already cleared.

A scope that does not fit even at 57 bytes is TRUNCATED to 48 and given an
8-hex-character suffix taken from its own SHA-256, rather than just a
shorter prefix: two DIFFERENT tenants truncated to the same 48 characters
must still not land on the same name, and the suffix has to come out the
same on every run of THIS tenant's own install — a random one would orphan
the role and the database an earlier run already created.
*/}}
{{- define "url-shortener-infra.postgresBase" -}}
{{- $scope := include "url-shortener-infra.eventsScope" . -}}
{{- $clean := regexReplaceAll "[^a-z0-9_]" (replace "-" "_" (lower $scope)) "" -}}
{{- if not (regexMatch "^[a-z]" $clean) -}}
{{- $clean = printf "t%s" $clean -}}
{{- end -}}
{{- $max := 57 -}}
{{- if le (len $clean) $max -}}
{{- $clean -}}
{{- else -}}
{{- printf "%s_%s" (trunc (sub $max 9 | int) $clean) (trunc 8 (sha256sum $scope)) -}}
{{- end -}}
{{- end -}}

{{/*
The database's own name, when a platform has not named its own — see
"url-shortener-infra.postgresBase" for where it comes from.
*/}}
{{- define "url-shortener-infra.postgresDatabase" -}}
{{- include "url-shortener-infra.postgresBase" . -}}
{{- end -}}

{{/*
The owner role migrates as, when a platform has not named its own — see
"url-shortener-infra.postgresBase".
*/}}
{{- define "url-shortener-infra.postgresOwnerRole" -}}
{{- printf "%s_owner" (include "url-shortener-infra.postgresBase" .) -}}
{{- end -}}

{{/*
The role every service connects as, when a platform has not named its own —
see "url-shortener-infra.postgresBase".
*/}}
{{- define "url-shortener-infra.postgresAppRole" -}}
{{- printf "%s_app" (include "url-shortener-infra.postgresBase" .) -}}
{{- end -}}

{{/*
The database name and both role names ACTUALLY USED, once
`postgres.tenantScopedNames` and whatever a caller set are both taken into
account.

`postgres.tenantScopedNames` is OFF BY DEFAULT (values.yaml), so every
existing consumer of this chart — a platform that only ever set
`postgres.runtimePasswordSecret` and relied on the three fixed defaults
below — keeps getting them, byte for byte, on every future release. It
exists for the one install that DOES need the tenant-scoped identifier
above: the local cluster's `test` tier, where the shared Postgres server
makes the fixed names collide between tenants exactly like an unscoped
stream would (see "url-shortener-infra.postgresBase").

"A caller did not set it" is not something a rendered template can ask
Helm directly — by the time `.Values` reaches here, a value left alone and
a value explicitly set back to the chart's own default already look
identical. So each of the three below compares the resolved value against
THIS CHART'S OWN literal default (values.yaml's `url_shortener` /
`_owner` / `_app`) rather than against emptiness: still exactly that
default, and scoping turned on, means "derive it"; anything else — set to
something else, and every branch when this is off — is used exactly as
given. That is also what makes an explicit name win over tenant-scoping
unconditionally: setting `postgres.database` to anything but its own
default opts that one field back out, whatever `tenantScopedNames` says.
*/}}
{{- define "url-shortener-infra.resolvedDatabase" -}}
{{- if and .Values.postgres.tenantScopedNames (eq .Values.postgres.database "url_shortener") -}}
{{- include "url-shortener-infra.postgresDatabase" . -}}
{{- else -}}
{{- .Values.postgres.database -}}
{{- end -}}
{{- end -}}

{{- define "url-shortener-infra.resolvedOwnerRole" -}}
{{- if and .Values.postgres.tenantScopedNames (eq .Values.postgres.ownerRole "url_shortener_owner") -}}
{{- include "url-shortener-infra.postgresOwnerRole" . -}}
{{- else -}}
{{- .Values.postgres.ownerRole -}}
{{- end -}}
{{- end -}}

{{- define "url-shortener-infra.resolvedAppRole" -}}
{{- if and .Values.postgres.tenantScopedNames (eq .Values.postgres.runtimeRole "url_shortener_app") -}}
{{- include "url-shortener-infra.postgresAppRole" . -}}
{{- else -}}
{{- .Values.postgres.runtimeRole -}}
{{- end -}}
{{- end -}}
