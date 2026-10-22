# Development commands. Tools come from devbox (`devbox shell`, or direnv);
# CI runs each recipe as its own job.
#
# `check` is the gate, and it needs no credentials and no cluster: only what
# the lock files name, which a first build downloads (the Go modules, the
# `service-lib` chart from ghcr.io, a Python and a Gradle dependency tree).
# A contributor runs exactly what CI runs on a pull request. The integration
# suite (`e2e-*`, `snapshot`) is the one thing that reaches a cluster, and it
# runs in .github/workflows/integration.yaml only, never on a pull request
# from a fork.
#
# `vuln` is NOT in `check`: a new advisory is news about the world, not about
# the change under review. It runs in .github/workflows/security.yaml.

# truvity/policy's module directory at the version go.mod pins: the shared
# schemas, the configuration fixtures and the contracts the loaders and the
# charts are held to are read from there, not copied here.
export POLICY_DIR := `hack/policy-dir.sh`

# Everything CI requires
[doc("Everything CI requires")]
check: build test lint drift leak-canary

# Compile everything
[doc("Compile everything")]
build:
    go build ./...
    # Resolving and installing is the Python components' build: it is what
    # proves the lock still describes something that exists.
    cd python && uv sync --frozen --quiet
    cd log && uv sync --frozen --quiet
    # The Kotlin loader and the counter. `assemble` rather than `build`, so
    # this stays a build: the tests are the test recipe's job.
    cd kotlin && gradle assemble --console=plain --quiet
    cd stat && gradle assemble --console=plain --quiet

# The unit tests. They need no network beyond the module and chart fetches,
# and no cluster; a local PostgreSQL (the devbox package) is started by the
# few tests that need one.
[doc("The unit tests")]
test:
    go test ./...
    # The Python loader, against truvity/policy's fixtures.
    cd python && uv run --frozen pytest -q
    # The archiver: what it decides about batching, keys and ordering, and
    # what its probes answer.
    cd log && uv run --frozen pytest -q
    # The Kotlin loader and the counter.
    cd kotlin && gradle test --console=plain --quiet
    cd stat && gradle test --console=plain --quiet

# Report known vulnerabilities in what this module depends on. Its own
# workflow (security.yaml), never the gate.
[doc("Report known vulnerabilities")]
vuln:
    govulncheck ./...

# Render and validate every chart against the Kubernetes API's own schemas.
#
# NOT part of `check`: it fetches the schema for a custom resource. It sits
# beside `ts` for the same reason.
[doc("Render and validate every chart")]
charts:
    bash hack/kubeconform.sh

# Regenerate the chart goldens. Read the diff BEFORE running this: a golden
# updated without being read is a golden that records whatever happened.
#
# The pattern matches all THREE chart render tests (the application chart, the
# infrastructure chart and the library-convention example chart).
[doc("Regenerate the chart goldens")]
golden:
    UPDATE_GOLDEN=1 go test ./charts/... -run 'TestWhatThe(Infra|ServiceExample)?ChartRenders' -count=1

# Regenerate the RPC code from the schema.
[doc("Regenerate the RPC code from the schema")]
protos:
    buf lint
    buf generate

# Compose the values schema of every service chart: the platform schema, and
# `config` as the service's own schema, bundled into one file Helm can validate
# with offline (package chartschema in truvity/policy). The source of each is
# the `values.schema.src.json` beside it. Needs no network.
[doc("Compose every service chart's values.schema.json from its source")]
chart-schemas:
    go run ./hack/compose-chart-schema charts

# Refresh the Chart.lock of every chart that has a dependency: the application
# chart's library (`service-lib`, from truvity/policy, at the version of the
# policy Go module in go.mod) and the infrastructure chart's cnpg-database. The
# lock is committed and the archive it resolves is not.
[doc("Refresh the Chart.lock of the charts that have a dependency")]
chart-locks:
    helm dependency update charts/url-shortener
    helm dependency update charts/url-shortener-infra
    helm dependency update charts/testdata/service-example

# Resolve what every chart declares, from its committed lock, and fail if the
# lock no longer says what Chart.yaml does. Run before anything here that
# renders a chart outside the Go tests (which resolve them themselves).
[doc("Resolve the chart dependencies from the committed locks")]
chart-deps:
    helm dependency build --skip-refresh charts/url-shortener
    helm dependency build --skip-refresh charts/url-shortener-infra

# Regenerate what the loaders carry from truvity/policy's `schemas/`.
[doc("Regenerate the schemas the loaders carry")]
schemas:
    node hack/generate-schemas.mjs

# Generated code is committed. A schema changed without regenerating would
# leave the loaders validating different documents.
[doc("Fail if generated code was not regenerated")]
drift: schemas protos chart-schemas chart-deps
    git diff --exit-code -- ts/src/schemas.generated.ts python/src/truvity_policy/schemas.py
    # The RPC code too. It is committed rather than generated at build time,
    # because a build step between a checkout and a compiler is a step that
    # has to work on every machine forever.
    git diff --exit-code -- internal/gen web/src/gen
    # The charts' values schemas, composed from their sources: committed
    # because a chart is published from the tree, and also held by a test that
    # does not need this recipe to have run.
    git diff --exit-code -- 'charts/*/values.schema.json' 'charts/testdata/*/values.schema.json'

# SHADOW PHASE of decision 0010 (truvity/policy docs/decisions): the data
# contracts are written in Pkl (contract/) and generated beside the
# hand-written schemas, which stay authoritative. Pkl is `bin/pkl`, a pinned,
# checksum-verified download; the contracts' packages are fetched from their
# GitHub release the first time, so these need the network and are NOT part of
# `check`.
#
# Regenerate the shadow directory: JSON Schemas, each chart's values schema,
# TypeScript with zod, Python with pydantic, the Markdown reference.
[doc("Regenerate the Pkl contract's shadow artifacts")]
shadow-generate:
    hack/shadow-generate.sh

# The name the fleet's Pkl pin-bump job looks for (truvity/ci-workflows
# pkl-fleet): after it moves the contracts pin and resolves it, it runs the
# repository's `generate` recipe, so a bump pull request carries regenerated
# shadow artifacts. It is a recipe and not an `alias` because the job finds
# recipes with `just --summary`, which does not list aliases.
[doc("Regenerate the Pkl contract's shadow artifacts (the name the pin-bump job runs)")]
generate: shadow-generate

# Strict: the committed shadow artifacts are exactly what the contract
# generates, the Pkl is in Pkl's canonical format, and the pinned packages are
# resolved (PklProject.deps.json is current).
[doc("Fail if the committed shadow artifacts are not what the contract generates")]
shadow-generated:
    #!/usr/bin/env bash
    set -euo pipefail
    bin/pkl format --diff-name-only contract
    bin/pkl project resolve contract >/dev/null
    git diff --exit-code -- contract/PklProject.deps.json
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    hack/shadow-generate.sh "$tmp/generated" >/dev/null
    diff -r "$tmp/generated" contract/generated
    echo "shadow-generated: contract/generated is current"

# REPORT ONLY. Generates afresh and compares with the hand-written schemas,
# semantically. It fails only if generation fails or a file it compares is
# missing, never on a difference. In CI the report goes to the job summary.
[doc("Report how the generated schemas differ from the hand-written ones (report only)")]
shadow-diff:
    #!/usr/bin/env bash
    set -euo pipefail
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    hack/shadow-generate.sh "$tmp/generated" >/dev/null
    # The Python loader's own environment, from its lock: it has jsonschema and
    # PyYAML, which the verdicts need, and nothing else is installed for it.
    uv run --frozen --no-dev --project python python hack/shadow-diff.py "$tmp/generated" \
        --report "$tmp/report.md" --summary "$tmp/summary.json"
    cat "$tmp/report.md"
    if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
        cat "$tmp/report.md" >> "$GITHUB_STEP_SUMMARY"
    fi

# The TypeScript loader and the web front end: install, typecheck, test,
# build. NOT part of `check`, which needs nothing but the checkout: this
# fetches from a registry. CI runs it as its own job.
[doc("The TypeScript loader and the front end: install, typecheck, test, build")]
ts:
    #!/usr/bin/env bash
    set -euo pipefail
    cd ts
    yarn install --immutable
    yarn typecheck
    yarn test --run
    rm -rf dist && yarn build

    # The front end, which depends on the loader just built. LAST, because
    # everything above runs in the loader's directory.
    cd ../web
    yarn install --immutable
    yarn typecheck
    yarn test --run
    yarn build

# The rules that hold for every file in this repository.
#
# Deliberately no `set -e`: a grep that finds nothing exits non-zero, and
# under `set -e` with `pipefail` that ends the recipe silently — a gate that
# reports nothing and fails is worse than no gate. Each check sets `fail`
# itself.
[doc("The rules that hold for every file")]
lint:
    #!/usr/bin/env bash
    set -uo pipefail
    fail=0

    # `config verify` FIRST: a settings block in the wrong place is accepted
    # silently by `run` and rejected only here, so without this a lint
    # setting can spend releases doing nothing.
    golangci-lint config verify || fail=1
    GOTOOLCHAIN=local golangci-lint run ./... || fail=1

    # The Kotlin compiler with warnings as errors, which is where a JVM
    # project's lint lives: a warning nobody fails on is a warning nobody
    # reads.
    for jvm in kotlin stat; do
        ( cd "$jvm" && gradle compileKotlin compileTestKotlin --console=plain --quiet ) || fail=1
    done

    # Python: one tool for lint and formatting, and a type checker, because an
    # annotation nothing checks is a comment that rots.
    for py in python log; do
        ( cd "$py" \
            && uv run --frozen ruff check . \
            && uv run --frozen ruff format --check . \
            && uv run --frozen mypy ) || fail=1
    done

    # TypeScript: one tool for lint AND formatting. Each package's own
    # biome.jsonc extends lint/biome.base.jsonc (copied from truvity/policy)
    # and excludes what it generates. Needs no `yarn install`: Biome reads
    # source, not node_modules, which is what keeps this in the hermetic gate.
    for tsdir in ts web; do
        ( cd "$tsdir" && biome check ) || fail=1
    done

    # The whitespace rules every file holds to (.editorconfig, copied from
    # truvity/policy): a final newline, LF, UTF-8, no stray trailing
    # whitespace but a Markdown hard break. Indentation WIDTH is deliberately
    # NOT checked here — gofmt, Biome, ruff and ktlint do that. The one
    # exclusion is the chart goldens: rendered output the `golden` recipe
    # regenerates byte for byte, never hand-edited.
    editorconfig-checker -disable-indentation -disable-indent-size \
        -exclude 'testdata/golden/' || fail=1

    # The toolchain is declared once and resolves to what it declares.
    python3 hack/toolchain-check.py || fail=1

    # Nothing built is committed. A binary in a public repository's history is
    # in every clone forever, and carries the build machine's paths. The
    # largest file here is prose; 1 MiB is a build output.
    big=$(git ls-files -s | awk '{print $2" "$4}' \
            | git cat-file --batch-check='%(objectsize) %(rest)' \
            | awk '$1 > 1048576 {print "    "$2}')
    if [ -n "$big" ]; then
        echo "LINT: build output does not belong in a public repository's history:"
        echo "$big"
        fail=1
    fi

    # Every relative link in a Markdown file resolves. A document nobody can
    # follow is a document nobody reads.
    broken=""
    while IFS= read -r -d '' f; do
        while read -r link; do
            case "$link" in http*|mailto:*|'') continue ;; esac
            target="$(dirname "$f")/${link%%#*}"
            [ -e "$target" ] || broken+="    $f -> $link"$'\n'
        done < <(grep -oE '\]\([^)]+\)' "$f" | sed 's/^](//;s/)$//')
    done < <(git ls-files -z '*.md')
    if [ -n "$broken" ]; then
        echo "LINT: a relative link does not resolve:"
        printf '%s' "$broken"
        fail=1
    fi

    [ "$fail" = 0 ] && echo "lint clean"
    exit $fail

# This repository is public and its history cannot be unpublished — a rewrite
# changes the SHAs but not what was already fetched. So the rule (mechanism
# only; particulars are the consuming estate's) is enforced mechanically
# rather than remembered. That includes the estate's host names: the
# integration suite takes them from the environment.
[doc("Refuse a particular that must never be published")]
leak-canary:
    hack/leak-canary.sh

# ---- The integration suite --------------------------------------------------
#
# The two doors (package e2e/devel): the Kubernetes API, and the release's
# public host. A person runs exactly what the workflow runs, after
# `sluisctl login`, with the repository variables exported (the same names:
# DEVEL_API_SERVER, DEVEL_API_CA, SLUIS_ISSUER, E2E_INSTALLS_DOMAIN,
# E2E_ARCHIVE_BUCKET, E2E_ARCHIVE_REGION).

# Write the kubeconfig the suite's first door uses
[doc("Write the kubeconfig for the devel cluster")]
kubeconfig:
    hack/kubeconfig.sh

# Build the images and package the charts exactly as a release does, for the
# integration run: images to ghcr.io as ci-<sha>, charts to dist/charts.
[doc("Build and push the images, package the charts (as a release does)")]
snapshot:
    hack/snapshot.sh

# Install the release into the CI namespace
[doc("Install the release into the CI namespace of the devel cluster")]
e2e-install:
    go run ./e2e/cmd/e2ectl sweep
    go run ./e2e/cmd/e2ectl install
    go run ./e2e/cmd/e2ectl wait

# Run the suite against the release `e2e-install` made
[doc("Run the end-to-end suite against the installed release")]
e2e-test:
    #!/usr/bin/env bash
    set -euo pipefail
    set -a
    eval "$(go run ./e2e/cmd/e2ectl env)"
    set +a
    go test ./e2e/suite/... -count=1 -v -timeout 20m

# Remove the release
[doc("Remove the release from the devel cluster")]
e2e-uninstall:
    go run ./e2e/cmd/e2ectl uninstall

# Install, test, and uninstall whatever happens
[doc("Install, test, uninstall")]
e2e: snapshot
    #!/usr/bin/env bash
    set -uo pipefail
    just e2e-install && just e2e-test
    status=$?
    just e2e-uninstall
    exit $status
