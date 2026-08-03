# nelmwave

Declarative release orchestrator on top of [nelm](https://github.com/werf/nelm)
(the werf team's Helm replacement). Spiritually a sibling of
[helmwave](https://github.com/helmwave/helmwave), but with a new schema and a
new engine.

`nelmwave` manages many releases from a single declarative `nelmwave.yml`
manifest: it renders the manifest through gomplate, resolves values and
companion files from arbitrary datasources, builds a dependency graph between
releases, and applies everything through nelm — in parallel, respecting order.

> **Status: early development (M1).** `build` renders a manifest, validates it,
> and writes a self-contained plan to `.nelmwave/`. `up`/`down`/`diff` are wired
> but not yet implemented — see the milestones in [`prompt.md`](./prompt.md).

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
  plan/              # .nelmwave/ plan build/read/write
  log/               # zap setup (auto console/json)
  version/           # build-time version info
```

Further packages (`datasource`, `graph`, `release`, `chart`, `kubedep`) land on
their respective milestones.

> **Note on confijer:** the manifest loader binds keys via `json` struct tags
> (case-insensitively), not `yaml` tags. Config structs therefore carry both
> `json` (for loading) and `yaml` (for plan serialization) tags in sync.
