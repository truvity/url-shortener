#!/usr/bin/env bash
# Generate everything the url-shortener's Pkl contract
# (contract) produces, into a directory:
#
#   <dir>/schemas/             JSON Schemas, one document per shape
#   <dir>/charts/<chart>/      each chart's values.schema.json and README.md;
#                              values.yaml too where the defaults are modelled
#   <dir>/ts/contract.zod.ts   TypeScript types and zod schemas
#   <dir>/py/contract_pydantic.py   pydantic v2 models
#   <dir>/docs/reference.md    the Markdown reference
#
#   hack/shadow-generate.sh [dir]     default: contract/generated
#
# SHADOW PHASE (README.md, "The contract in Pkl"): nothing here is read by a service,
# a chart or a test. The hand-written schemas stay authoritative.
#
# Every generator runs as a PACKAGE of the pinned contracts release, the way a
# consumer runs it; the version is the one the PklProject pins, read from there
# so that it is written once. Go and Kotlin are not generated yet: they come
# from Pkl's official generators (pkl-contracts' hack/codegen.sh), which need a
# further download each, and their types carry none of the constraints, so they
# would add nothing to a comparison of schemas.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
cd "$here"

project=contract
out="${1:-$project/generated}"
pkl="$here/bin/pkl"

version="$(sed -n 's/.*contracts\.vocab@\([0-9][0-9.]*\)".*/\1/p' "$project/PklProject")"
[ -n "$version" ] || { echo "shadow-generate: no pinned contracts version in $project/PklProject" >&2; exit 1; }
base="package://github.com/truvity/pkl-contracts/releases/download/v$version"
frag="projectpackage://github.com/truvity/pkl-contracts/releases/download/v$version/contracts.fragments@$version"

# Relative to the project. The fragments and the platform block are named too:
# the schemas are generated from what the components REFER to, and a fragment
# none of them refers to (postgres) would otherwise have no generated twin.
configs=(config/Archiver.pkl config/Echo.pkl config/Migrate.pkl config/Prober.pkl
  config/Redirect.pkl config/Stat.pkl config/Urls.pkl config/Web.pkl
  "$frag#/Fragments.pkl" "$frag#/Platform.pkl")
charts=(charts/ServiceExample.pkl charts/UrlShortener.pkl
  charts/UrlShortenerInfra.pkl)

# A relative --dir is relative to the working directory; make it absolute so
# that the project directory does not change what it means.
case "$out" in /*) ;; *) out="$here/$out" ;; esac
rm -rf "$out"

run() { # generator, then its arguments
  local g="$1"
  shift
  local log
  log="$("$pkl" run --project-dir "$here/$project" "$base/contracts.$g@$version#/Generate.pkl" -- "$@" 2>&1)" ||
    {
      echo "shadow-generate: the $g generator failed" >&2
      echo "$log" >&2
      return 1
    }
}

cd "$here/$project"
run jsonschema --dir "$out/schemas" "${configs[@]}"
run typescript --dir "$out/ts" "${configs[@]}" "${charts[@]}"
run python --dir "$out/py" "${configs[@]}" "${charts[@]}"
run docs --dir "$out/docs" "${configs[@]}" "${charts[@]}"

# A chart: the contract gives the schema and the table. Where the defaults are
# modelled (a module that amends the contract with them) it gives values.yaml
# as well, and a default that breaks a rule fails here.
run helm --dir "$out/charts/service-example" charts/ServiceExampleValues.pkl
run helm --dir "$out/charts/url-shortener" charts/UrlShortener.pkl
run helm --dir "$out/charts/url-shortener-infra" charts/UrlShortenerInfra.pkl
echo "shadow-generate: wrote ${out#"$here"/}"
