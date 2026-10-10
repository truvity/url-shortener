{{/*
THE ONE PLACE this chart maps its release-wide values onto a component.

Every component is rendered by service-lib from two dicts and nothing else:
`platform` (what the platform schema describes) and `config` (the component's
own configuration file, exactly as its schema describes it). service-lib
renders `config` verbatim — it adds, drops and renames nothing — so what a
binary reads is what is built here, and the chart's tests validate it with the
schema the binary validates with at start-up.

Why this chart builds `config` at all, when a chart with ONE service hands the
library its own `.Values.config` (docs/guides/charts.md): this is a product
chart. Five components are wired to each other, and what they must agree on
is DERIVED — the stream and subject names from the namespace and the install
name, each caller's account from the release name, the address of `urls`
from its transport mode. A literal in a values file cannot be derived from
the release it is installed into, and agreement by formula is what keeps two
installs from finding each other's stream. The derivation is here, in one
template, and it ends at a dict: nothing below it reads a value.

Returns JSON: {redirect: {platform, config}, urls: ..., web: ..., stat: ...,
log: ...}. Takes the root context.
*/}}
{{- define "url-shortener.components" -}}
{{- $out := dict -}}
{{- range $name := list "redirect" "urls" "web" "stat" "log" -}}
{{- $arg := dict "root" $ "component" $name -}}
{{- $_ := set $out $name (dict "platform" (include "url-shortener.platform" $arg | fromJson) "config" (include "url-shortener.config" $arg | fromJson)) -}}
{{- end -}}
{{- toJson $out -}}
{{- end -}}

{{/*
What the platform provides a component. Takes (dict "root" $ "component" <n>).

  - the image is NOT here: service-lib reads this chart's own top-level
    `images.<component>`, the map a release stamps and refuses to publish
    with an empty digest;
  - `drain` is ONE number, used three times: the service's own timeout is
    `config.drain.seconds`, and service-lib derives the grace period and the
    pre-stop delay from it (see service-lib's own comment);
  - `tls.mount` is the release's, not the component's: every workload mounts
    the identity once the transport is on at all, including the ones whose own
    file names no certificate, which is what lets the account that asks for it
    be granted by one RBAC rule for the release.
*/}}
{{- define "url-shortener.platform" -}}
{{- $root := .root -}}
{{- $v := $root.Values -}}
{{- $name := .component -}}
{{- $p := dict
  "imagePullPolicy" $v.pullPolicy
  "replicas" (int (include "url-shortener.replicas" (dict "root" $root "component" $name)))
  "resources" (get $v.resources $name)
  "drain" (dict "preStopSeconds" $v.drain.preStopSeconds)
  "podSecurity" $v.podSecurity
  "tls" (dict "csiDriver" $v.tls.csiDriver "mountPath" $v.tls.mountPath "mount" (not (not (include "url-shortener.tlsOn" $root))))
  "telemetry" ($v.otel | default dict)
  "serviceAccount" (dict "name" (include "url-shortener.componentServiceAccountName" .))
-}}
{{- $env := list -}}
{{- $volumes := list -}}
{{- $mounts := list -}}
{{- if or (eq $name "redirect") (eq $name "urls") -}}
{{- /* The two services that own or read the tables: the database client's
environment, its root and its password. `web` holds no database credential —
the ownership rule seen from the consuming side. */ -}}
{{- $env = include "url-shortener.dbEnv" (dict "root" $root "role" $v.database.app "component" $name) | fromYamlArray -}}
{{- /* What `database.maxConnections` was before the connection moved to the
client's environment: this component's share of the server's limit. */ -}}
{{- $env = append $env (dict "name" "CNPG_CLIENT_POOL_MAX" "value" (ternary "10" "20" (eq $name "redirect"))) -}}
{{- $volumes = concat $volumes (include "url-shortener.dbCAVolume" $root | fromYamlArray) (include "url-shortener.dbPasswordVolume" $v.database.app | fromYamlArray) -}}
{{- $mounts = concat $mounts (include "url-shortener.dbCAMount" $root | fromYamlArray) (include "url-shortener.dbPasswordMount" $root | fromYamlArray) -}}
{{- end -}}
{{- if eq $name "redirect" -}}
{{- if include "url-shortener.eventsIdentityOn" $root -}}
{{- $volumes = concat $volumes (include "url-shortener.eventsCAVolume" $root | fromYamlArray) -}}
{{- $mounts = concat $mounts (include "url-shortener.eventsCAMount" $root | fromYamlArray) -}}
{{- else -}}
{{- $volumes = concat $volumes (include "url-shortener.eventsTokenVolume" $root | fromYamlArray) -}}
{{- $mounts = concat $mounts (include "url-shortener.eventsTokenMount" $root | fromYamlArray) -}}
{{- end -}}
{{- end -}}
{{- if or (eq $name "stat") (eq $name "log") -}}
{{- $volumes = concat $volumes (include "url-shortener.eventsTokenVolume" $root | fromYamlArray) -}}
{{- $mounts = concat $mounts (include "url-shortener.eventsTokenMount" $root | fromYamlArray) -}}
{{- end -}}
{{- if eq $name "log" -}}
{{- with $v.archive.bucket.credentialsSecret -}}
{{- /* The NAMES are in the configuration file (`credentialsSecret`); the
values are here, from a Secret, projected as files under `secrets.root`, and
appear in nothing that is rendered, printed or committed. */ -}}
{{- $bucket := $v.archive.bucket -}}
{{- $_ := set $p "secretFiles" (dict "s3/accessKeyID" (dict "secretName" . "key" $bucket.accessKeyIDKey) "s3/secretAccessKey" (dict "secretName" . "key" $bucket.secretAccessKeyKey)) -}}
{{- end -}}
{{- end -}}
{{- with (get ($v.jvmOptions | default dict) $name) -}}
{{- $env = append $env (dict "name" "JAVA_TOOL_OPTIONS" "value" .) -}}
{{- end -}}
{{- if $env -}}{{- $_ := set $p "env" $env -}}{{- end -}}
{{- if $volumes -}}{{- $_ := set $p "volumes" $volumes -}}{{- end -}}
{{- if $mounts -}}{{- $_ := set $p "volumeMounts" $mounts -}}{{- end -}}
{{- toJson $p -}}
{{- end -}}

{{/*
The `tls` block of a component that SERVES an authenticated boundary: `urls`
and `redirect`. Takes (dict "root" $ "component" <n> "peers" <list>); returns
JSON, `null` while the transport is off for that component.

toJson rather than text, so an EMPTY allow-list is `[]` and never a key with
nothing under it. The second is YAML null, the service refuses it at start-up
("tls.peers: got null, want array"), and the pod crash-loops — and the state
that triggers it is the DEFAULT, because an empty allow-list is what a
service nobody has been granted looks like.
*/}}
{{- define "url-shortener.servingTLS" -}}
{{- $v := .root.Values -}}
{{- $mode := include "url-shortener.tlsMode" (dict "root" .root "component" .component) -}}
{{- if ne $mode "off" -}}
{{- $tls := dict
  "mode" $mode
  "certFile" (printf "%s/tls.crt" $v.tls.mountPath)
  "keyFile" (printf "%s/tls.key" $v.tls.mountPath)
  "caFile" (printf "%s/ca.crt" $v.tls.mountPath)
  "trustDomain" (required "tls.trustDomain is required once tls.mode is not off: without it a peer from any trust domain is admitted" $v.tls.trustDomain)
  "peers" .peers
-}}
{{- if eq $mode "permissive" -}}
{{- $_ := set $tls "address" (printf ":%v" $v.tls.port) -}}
{{- end -}}
{{- toJson $tls -}}
{{- else -}}
null
{{- end -}}
{{- end -}}

{{/*
The `tls` block of a component that only CALLS the URL service (`web`,
`stat`): `strict` whenever the transport is on at all, including while the rest
of the release is `permissive` — and that is not a copy-paste slip.

These SERVE nothing on the boundary. For a component that only calls out there
is no third state: it either presents an identity or it does not, and
`permissive` is meaningless because there is no second listener to put
anywhere. Rendering it produced a crash loop with an error about a listener
address on a component that has none. `permissive` exists so an edge can
migrate one CALLER at a time, and this chart wires both ends of this call.

`peers` is who this component will ACCEPT AN ANSWER FROM, which is NOT the
same question as who may call the URL service and must not share a list with
it: one is "may this caller in", the other is "is the thing that answered the
service I meant to reach". It is the URL service's OWN account.
*/}}
{{- define "url-shortener.clientTLS" -}}
{{- $v := .Values -}}
{{- if include "url-shortener.urlsClientTLSOn" . -}}
{{- toJson (dict
  "mode" "strict"
  "certFile" (printf "%s/tls.crt" $v.tls.mountPath)
  "keyFile" (printf "%s/tls.key" $v.tls.mountPath)
  "caFile" (printf "%s/ca.crt" $v.tls.mountPath)
  "trustDomain" (required "tls.trustDomain is required once tls.mode is not off: without it a peer from any trust domain is admitted" $v.tls.trustDomain)
  "peers" (list (dict "namespace" .Release.Namespace "serviceAccount" (include "url-shortener.componentServiceAccountName" (dict "root" . "component" "urls"))))
) -}}
{{- else -}}
null
{{- end -}}
{{- end -}}

{{/*
Where the URL service is, for the two components that call it. Which PORT
depends on its mode, because that is what decides where its authenticated
listener is: under `strict` it is the ordinary port speaking TLS, and under
`permissive` it is a second one beside the cleartext original.
*/}}
{{- define "url-shortener.urlsAddress" -}}
{{- $mode := include "url-shortener.tlsMode" (dict "root" . "component" "urls") -}}
{{- if eq $mode "strict" -}}
https://{{ include "url-shortener.name" . }}-urls:8080
{{- else if eq $mode "permissive" -}}
https://{{ include "url-shortener.name" . }}-urls:{{ .Values.tls.port }}
{{- else -}}
http://{{ include "url-shortener.name" . }}-urls:8080
{{- end -}}
{{- end -}}

{{/*
The broker, as a component reads it. The broker authenticates its clients, or
it does not; a token is a projected ServiceAccount token this pod cannot
forge. `identity` is redirect alone: its certificate is the credential, with
no token file, so a certificate the broker cannot map is refused instead of
falling through to the token path.
*/}}
{{- define "url-shortener.natsConfig" -}}
{{- $v := .root.Values -}}
{{- $nats := dict "url" $v.events.url -}}
{{- $identity := "" -}}
{{- if .identity -}}{{- $identity = include "url-shortener.eventsIdentityOn" .root -}}{{- end -}}
{{- if $identity -}}
{{- $tls := dict "caFile" (printf "/var/run/events-ca/%s" $v.events.tls.caKey) -}}
{{- with $v.events.tls.serverName -}}{{- $_ := set $tls "serverName" . -}}{{- end -}}
{{- $_ := set $nats "tls" $tls -}}
{{- else -}}
{{- with $v.events.auth.audience -}}{{- $_ := set $nats "tokenFile" "/var/run/events/token" -}}{{- end -}}
{{- end -}}
{{- toJson $nats -}}
{{- end -}}

{{/*
A component's configuration file, as a dict, for the schema the binary
validates against (schemas/*.json). Takes (dict "root" $ "component" <n>);
returns JSON. Only what the binary's schema names is here: no secret, and no
database either — the connection is the libpq environment (platform.env) and
the password is a mounted file the client re-reads.
*/}}
{{- define "url-shortener.config" -}}
{{- $root := .root -}}
{{- $v := $root.Values -}}
{{- $name := .component -}}
{{- $cfg := dict
  "probes" (dict "address" ":7070")
  "log" (dict "level" $v.log.level)
  "drain" (dict "seconds" $v.drain.seconds)
-}}
{{- if or (eq $name "redirect") (eq $name "urls") (eq $name "web") -}}
{{- $_ := set $cfg "listen" (dict "address" ":8080") -}}
{{- end -}}
{{- if eq $name "redirect" -}}
{{- $tls := include "url-shortener.servingTLS" (dict "root" $root "component" "redirect" "peers" ($v.tls.peers.redirect | default list)) | fromJson -}}
{{- if $tls -}}{{- $_ := set $cfg "tls" $tls -}}{{- end -}}
{{- $_ := set $cfg "events" (dict
  "nats" (include "url-shortener.natsConfig" (dict "root" $root "identity" true) | fromJson)
  "redirectSubject" (include "url-shortener.redirectSubject" $root)
  "requestSubject" (include "url-shortener.requestSubject" $root)) -}}
{{- else if eq $name "urls" -}}
{{- /*
This release's OWN callers first, and then whoever the operator added.

The chart knows its internal callers — it is the thing that wired them to this
service — so it grants them, and `tls.peers.urls` is for callers OUTSIDE the
release. Leaving the internal ones to the operator would mean a chart whose
default configuration cannot talk to itself: every install of `strict` comes up
with the counter refused at the handshake, and the error names a certificate
rather than a list nobody filled in. They are exactly `web` and `stat`, each as
its own account; `redirect` and `log` never call it, so they are not admitted.
*/ -}}
{{- $peers := list -}}
{{- range (list "web" "stat") -}}
{{- $peers = append $peers (dict "namespace" $root.Release.Namespace "serviceAccount" (include "url-shortener.componentServiceAccountName" (dict "root" $root "component" .))) -}}
{{- end -}}
{{- $peers = concat $peers ($v.tls.peers.urls | default list) -}}
{{- $tls := include "url-shortener.servingTLS" (dict "root" $root "component" "urls" "peers" $peers) | fromJson -}}
{{- if $tls -}}{{- $_ := set $cfg "tls" $tls -}}{{- end -}}
{{- else if eq $name "stat" -}}
{{- $tls := include "url-shortener.clientTLS" $root | fromJson -}}
{{- if $tls -}}{{- $_ := set $cfg "tls" $tls -}}{{- end -}}
{{- $_ := set $cfg "urls" (dict "address" (include "url-shortener.urlsAddress" $root)) -}}
{{- $_ := set $cfg "events" (dict
  "nats" (include "url-shortener.natsConfig" (dict "root" $root "identity" false) | fromJson)
  "consumer" (dict "stream" (include "url-shortener.eventsStream" $root) "durable" (include "url-shortener.statConsumer" $root) "subject" (include "url-shortener.redirectSubject" $root))) -}}
{{- else if eq $name "web" -}}
{{- with $v.web.csp -}}
{{- $csp := dict "mode" (.mode | default "report-only") "connectSrc" (.connectSrc | default list) -}}
{{- with .reportUri -}}{{- $_ := set $csp "reportUri" . -}}{{- end -}}
{{- $_ := set $cfg "csp" $csp -}}
{{- end -}}
{{- with $v.web.faro -}}
{{- if .enabled -}}
{{- $faro := dict "enabled" true "collectorUrl" (.collectorUrl | default "/faro/collect") "appName" (.appName | default "url-shortener-web") "sampleRate" (.sampleRate | default 1) -}}
{{- with .apiKey -}}{{- $_ := set $faro "apiKey" . -}}{{- end -}}
{{- with .environment -}}{{- $_ := set $faro "environment" . -}}{{- end -}}
{{- $_ := set $cfg "faro" $faro -}}
{{- end -}}
{{- end -}}
{{- $tls := include "url-shortener.clientTLS" $root | fromJson -}}
{{- if $tls -}}{{- $_ := set $cfg "tls" $tls -}}{{- end -}}
{{- $_ := set $cfg "urls" (dict "address" (include "url-shortener.urlsAddress" $root)) -}}
{{- $_ := set $cfg "assets" (dict "directory" "/app/assets") -}}
{{- else if eq $name "log" -}}
{{- $bucket := $v.archive.bucket -}}
{{- $b := dict "name" (required "archive.bucket.name is required: a component that archives to a bucket nobody named has nowhere to write" $bucket.name) -}}
{{- with $bucket.region -}}{{- $_ := set $b "region" . -}}{{- end -}}
{{- with $bucket.endpoint -}}{{- $_ := set $b "endpoint" . -}}{{- end -}}
{{- with $bucket.ca -}}{{- $_ := set $b "ca" . -}}{{- end -}}
{{- if $bucket.pathStyle -}}{{- $_ := set $b "pathStyle" true -}}{{- end -}}
{{- if $bucket.credentialsSecret -}}
{{- $_ := set $b "credentialsSecret" (dict "accessKeyID" "s3/accessKeyID" "secretAccessKey" "s3/secretAccessKey") -}}
{{- $_ := set $cfg "secrets" (dict "source" "file" "root" "/var/run/secrets/log") -}}
{{- end -}}
{{- $_ := set $cfg "events" (dict
  "nats" (include "url-shortener.natsConfig" (dict "root" $root "identity" false) | fromJson)
  "consumer" (dict "stream" (include "url-shortener.eventsStream" $root) "durable" (include "url-shortener.logConsumer" $root) "subject" (include "url-shortener.requestSubject" $root))) -}}
{{- $_ := set $cfg "archive" (dict "bucket" $b "prefix" $v.archive.prefix "batch" (dict "maxRecords" $v.archive.batch.maxRecords "maxSeconds" $v.archive.batch.maxSeconds)) -}}
{{- end -}}
{{- toJson $cfg -}}
{{- end -}}
