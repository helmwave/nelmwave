# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## graphify

This project uses a knowledge graph at `graphify-out/` with god nodes, community
structure, and cross-file relationships. **The graph is a local artifact:
`graphify-out/` is gitignored, so every contributor builds their own.** Nothing
in the repo depends on it — the rules below are conditional on it existing.

First-time setup (graphify 0.9.32+; AST extraction needs no API key):

```sh
graphify extract . --code-only   # build graphify-out/ for this checkout
graphify hook install            # optional: rebuild the graph on every commit
```

`graphify hook install` also appends a `graphify-out/graph.json merge=graphify`
line to `.gitattributes` for a file this repo does not track — drop that line if
it appears. Do not run `graphify claude install`: it rewrites the PreToolUse hook
in `.claude/settings.json` with an absolute path to your own binary, and that
file is tracked here in a machine-independent form (`graphify` resolved from
`PATH`, no-op when it is not installed).

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).

## Commands

```sh
make build              # go build with version/commit/date stamped via -ldflags
make test               # unit tests (no cluster); == go test ./...
make lint               # golangci-lint, config pinned in .golangci.yml
make examples           # build every examples/*/nelmwave.yml.tpl — catches schema drift
make version            # print the metadata `make build` would stamp

go test ./internal/cli -run TestDeploy_InstallsInDependencyOrder   # a single test
go test ./internal/plan -bench . -run '^$'                         # benchmarks (also in build/, datasource/)
```

Plain `go build ./cmd/nelmwave` works but leaves `version.Version` at its fallback; use `make build` when the version matters.

End-to-end suite (real k3s cluster in docker-compose, behind the `e2e` build tag so `go test ./...` never reaches for a cluster):

```sh
make e2e                # up + test + down (tears down even on failure)
make e2e-up             # start k3s, wait for healthcheck
make e2e-test           # run the suite, repeatable
make e2e-down           # remove container and volumes
```

With podman, export `DOCKER_HOST` first (see the Makefile header) and delegate the `cpuset` cgroup controller (see README "Using podman?").

Requires Go 1.26+.

## Architecture

`nelmwave` reads a declarative `nelmwave.yml[.tpl]` manifest describing many
releases and applies them through [nelm](https://github.com/werf/nelm) in
dependency order. Full user-facing schema and flag reference lives in
[README.md](./README.md); this section is the part you cannot see from one file.

### The build/apply split is the central invariant

`build` is the **only** phase that renders templates or touches datasources. It
writes a self-contained artifact directory (`.nelmwave/`: `planfile.yml`,
`values/<uniqname>/`, `stores/<uniqname>/`). `up`, `down` and `diff` read that
plan and never re-render, so what was reviewed is what gets applied. Never add
template rendering or datasource resolution to a runtime command path.

Pipeline: `cli/build.go: buildPlan()` → `tpl.Render` (gomplate, `[[ ]]`) →
`config.Parse` → `config.Validate` → `plan.FromConfig` → `build.Artifacts`
(resolves values/stores into `.nelmwave/`) → `plan.Write`.

Runtime path: `plan.Read` → `cli/deploy.go: deploy()` → label selection →
`--include-needs` expansion / required-need check → `graph.Reverse` for `down` →
`repo.Resolve` + `buildSpec` → `graph.Run` over `release.Applier`.

### Package roles

| Package | Role |
|---|---|
| `internal/cli` | cobra tree; `deploy.go` holds the shared up/down/diff engine and is the largest hub |
| `internal/config` | manifest schema, confijer loading, normalization, validation, uniqnames, selectors |
| `internal/tpl` | gomplate v5 rendering with `[[ ]]` delimiters (Helm's `{{ }}` stays untouched) |
| `internal/datasource` | turn one `FileRef.Src` into bytes; behaviour chosen by extension |
| `internal/build` | drive the resolver over every release, write artifacts, register cross-reference datasources |
| `internal/plan` | the on-disk plan: `FromConfig`, `Write`, `Read` |
| `internal/graph` | concurrent DAG executor |
| `internal/release` | `Spec`/`Applier` abstraction and the nelm implementation; namespace metadata; kube connection |
| `internal/repo` | resolve a chart ref against declared repositories; generate temp OCI `config.json` |

No import cycles; `cli` depends on everything else, nothing depends on `cli`.

### Things that will bite you

**confijer binds by `json` tags, not `yaml`.** Config structs in
`internal/config` carry both: `json` for loading the manifest, `yaml` for plan
serialization. Keep them in sync — a missing `json` tag means the field silently
never loads. The top-level `Release:` / `Namespace:` blocks are confijer *type
defaults* keyed by Go type name; maps deep-merge, lists are replaced.

**confijer drops a scalar where it expects a struct**, silently. That is why
`config.Parse` pre-normalizes the raw YAML tree before decoding
(`normalizeRefLists`, `normalizeRepositories`, `normalizeLabels`,
`checkNamespaceBlocks` in `load.go`). Any new sugar spelling ("bare string means
`{src: ...}`") needs a normalizer there, and any shape that must be rejected
rather than dropped needs an explicit check.

**Release identity lives entirely in the map key** — uniqname
`name[@namespace[@kubecontext]]`. A release body has no name/namespace fields.
`canonicalizeUniqnames` normalizes keys *and* `needs` entries so equivalent
spellings collapse and collisions error out.

**Artifact resolution is deliberately sequential.** gomplate v5 writes to a
package-level `Metrics` struct without synchronization, so concurrent `Render`
calls crash with "concurrent map writes" (see the comment on
`build.Artifacts`). Do not parallelize it until gomplate is safe.

**Datasource behaviour comes from the extension, not the scheme**: `.yml` copied
verbatim, `.yml.tpl` rendered, `.yml.sops` decrypted in-process (no `sops`
binary), `.yml.tpl.sops` decrypted then rendered. Within a release, `stores`
resolve before `values`, and each resolved artifact is registered as a gomplate
datasource (`stores/<name>`, `values/<name>`) visible to *later* artifacts of the
same release only.

**`graph.Run` is fail-fast per branch, not globally.** A failed node skips its
dependents and returns `Result{Skipped: true}` for them; unrelated branches keep
running. `Run` never returns early — the caller (`summarize`) aggregates.

**Exit codes flow through `exitError`.** `diff --detailed-exitcode` reports
planned changes as code 2 without being a failure; `cli.ExitCode(err)` is the
public mapping for embedders (the e2e suite uses it).

**Secrets never belong in the manifest.** `build` copies the manifest into
`planfile.yml`, so anything embedded there lands in cleartext on disk and in CI
artifacts. Cluster credentials are flags (`internal/cli/kube.go`), and
`driverURL` passwords get a warning (`warnEmbeddedDriverPasswords`).

**`go.mod` `replace` directives are load-bearing.** k8s.io/* pinned to 0.29.x,
`docker/cli` back to v25 (oras-go vs sops conflict), `imdario/mergo` back to
v0.3.6 (kubeconfig merge order must stay "first wins", like kubectl). Do not
bump these without reading the comments there.

### Adding a release field

A new manifest field usually touches, in order: `config/release.go` (with both
tag sets) → `config/validate.go` → `plan/plan.go` (`Release` + `FromConfig`) →
`release/release.go` (`Spec`) → `release/nelm.go` (map onto nelm's options) →
tests → README table → an `examples/` project if it needs demonstrating. Then
`make examples` proves nothing went stale.

## Testing conventions

Unit tests drive the real cobra tree and the real plan machinery, substituting
only the engine: `fakeApplier` in `internal/cli/deploy_test.go` records the
`release.Spec`s it was handed, so ordering, selection, need-expansion and
spec-mapping are all asserted without a cluster. Prefer extending that pattern
over mocking further up. Benchmarks live in `bench_test.go` under `plan`
(validate/project 200 releases), `build` (artifacts for 50 releases) and
`datasource` (single-ref resolve against an `os.ReadFile` baseline).

Commit messages in this repo are `feat: <short summary>`.
