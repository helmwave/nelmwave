# nelmwave

Declarative release orchestrator on top of [nelm](https://github.com/werf/nelm)
(the werf team's Helm replacement). Spiritually a sibling of
[helmwave](https://github.com/helmwave/helmwave), but with a new schema and a
new engine.

`nelmwave` manages many releases from a single declarative `nelmwave.yml`
manifest: it renders the manifest through gomplate, resolves values and
companion files from arbitrary datasources, builds a dependency graph between
releases, and applies everything through nelm — in parallel, respecting order.

> **Status: early development (M4).** `build` renders/validates a manifest and
> resolves values/store datasources into `.nelmwave/`. `up`/`down` select
> releases by label, build the dependency DAG, and apply them through nelm — in
> parallel, respecting order (`down` in reverse). `diff` (alias `plan`) shows
> pending changes (`--detailed-exitcode` exits 2 when changes are planned);
> `up --dry-run` delegates to it. Charts resolve against the declared
> `repositories` (helm repos and OCI registries, with credentials). See the
> milestones in [`prompt.md`](./prompt.md).

## Building

```sh
go build ./cmd/nelmwave
```

Requires Go 1.26+.

## Commands (MVP)

| Command | Purpose |
|---|---|
| `nelmwave build` | Render `nelmwave.yml.tpl` and write the plan to `.nelmwave/` |
| `nelmwave up`    | Deploy the selected releases in dependency order |
| `nelmwave down`  | Uninstall the selected releases in reverse order |
| `nelmwave diff`  | Show the changes that would be applied (alias: `plan`) |

Global flags: `--log-level`, `--log-format` (auto/console/json),
`--kube-context`, `--kube-config`, `--version`.

Release selection uses Kubernetes-style label selectors:

```sh
nelmwave up -l 'app=api,env in (prod,stg),tier!=db'
```

## Try the build

```sh
cd examples/quickstart
ENV=stg go run github.com/helmwave/nelmwave/cmd/nelmwave build
cat .nelmwave/planfile.yml
```

Manifests are rendered by gomplate v5 using `[[ ]]` action delimiters; the
render context exposes `.Env`/`getenv` and gomplate's standard function
namespaces (`strings`, `datasource`, ...).

## Layout

```
cmd/nelmwave/        # main(): CLI entry point
internal/
  cli/               # cobra commands: build, up, down, diff
  config/            # nelmwave.yml schema, confijer load, validation, selectors
  tpl/               # gomplate v5 rendering ([[ ]] delimiters)
  datasource/        # resolve values/store refs (gomplate v5)
  build/             # resolve a config's datasources into .nelmwave/ artifacts
  plan/              # .nelmwave/ plan build/read/write
  graph/             # concurrent dependency-DAG executor
  release/           # Applier over nelm (install/uninstall/plan)
  repo/              # resolve chart refs against repositories; OCI docker config
  log/               # zap setup (auto console/json)
  version/           # build-time version info
```

Further packages (`kubedep` for resource-level ordering) land on their
respective milestones.

> **Note on confijer:** the manifest loader binds keys via `json` struct tags
> (case-insensitively), not `yaml` tags. Config structs therefore carry both
> `json` (for loading) and `yaml` (for plan serialization) tags in sync.
