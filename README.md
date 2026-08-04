# nelmwave

Declarative release orchestrator on top of [nelm](https://github.com/werf/nelm)
(the werf team's Helm replacement). Spiritually a sibling of
[helmwave](https://github.com/helmwave/helmwave), but with a new schema and a
new engine.

`nelmwave` manages many releases from a single declarative `nelmwave.yml`
manifest: it renders the manifest through gomplate, resolves values and
companion files from arbitrary datasources, builds a dependency graph between
releases, and applies everything through nelm — in parallel, respecting order.

> **Status: MVP.** `build`, `up`, `down` and `diff` are implemented, along with
> datasource resolution, the dependency DAG, label selection, and chart
> resolution against helm repositories and OCI registries.

## Building

```sh
go build ./cmd/nelmwave
```

Requires Go 1.26+.

## Quick start

```sh
cd examples/quickstart
ENV=stg nelmwave build
cat .nelmwave/planfile.yml
```

`build` needs no cluster and downloads no charts — it renders, validates and
resolves everything locally into `.nelmwave/`. `up`, `down` and `diff` need a
Kubernetes cluster.

See [`examples/`](./examples) for runnable projects.

---

## The manifest

The manifest is `nelmwave.yml.tpl` (or a plain `nelmwave.yml`), and looks like
this:

```yaml
project: my-platform

repositories:
  bitnami: https://charts.bitnami.com/bitnami
  private:
    url: oci://registry.example.com
    username: [[ .Env.REGISTRY_USER ]]
    password: [[ .Env.REGISTRY_PASS ]]

Release:                      # defaults applied to every release
  labels:
    common: true

releases:
  postgres@data:
    labels: { app: postgres, tier: db }
    chart:
      name: bitnami/postgresql
      version: 15.x
    values:
      - values/pg.yml.tpl

  api@app:
    labels: { app: api, tier: backend }
    needs:
      releases:
        postgres@data: { strict: true }
    chart:
      name: oci://registry.example.com/charts/api
      version: 1.4.2
    values:
      - src: values/api.yml.tpl
    sets:
      replicaCount: 3
      image.tag: [[ getenv "API_TAG" "1.4.2" ]]
    stores:
      - { src: extra/netpol.yml, name: netpol.yml }
```

### Templating

A `.tpl` manifest is rendered by [gomplate v5](https://gomplate.ca) using
**`[[ ]]`** action delimiters (so Helm's own `{{ }}` stays untouched). The render
context exposes `.Env` / `getenv` and gomplate's standard function namespaces
(`strings`, `datasource`, `conv`, ...). A plain `nelmwave.yml` is loaded
verbatim, without rendering.

Only `build` renders. `up`, `down` and `diff` read the plan it wrote, so the
manifest cannot change between review and apply.

### `project`

A free-form name for the whole manifest. Carried into the plan; not sent to the
cluster.

### `repositories`

A map keyed by alias (helm repos) or host (OCI registries). The URL scheme
decides which is which: `https://` is a classic helm repository, `oci://` an OCI
registry. A value is either a bare URL string or an object:

| Field | Meaning |
|---|---|
| `url` | Repository index URL or OCI registry URL. Required. |
| `username`, `password` | Basic-auth credentials. |
| `force_update` | Re-fetch the repo index even if cached (helm repos). |
| `insecure_skip_tls_verify` | Disable TLS verification for this repo. |
| `pass_credentials` | Forward credentials to all domains, not just the repo host. |
| `ca_file` | Path to a CA bundle for this repo. |

There is no `repositories.yaml` step: a helm-repo chart is fetched helm
`--repo` style (chart name plus repo URL), and OCI credentials are handed to nelm
through a generated, temporary `config.json`.

### `releases`

A map keyed by **uniqname** — `name[@namespace[@kubecontext]]`:

```yaml
releases:
  api:                      # current context, its default namespace
  api@app:                  # namespace app
  api@app@staging:          # namespace app, kube-context staging
```

Namespace and kube-context are optional; when omitted the current kube-context
and its default namespace are used, resolved at apply time. The identity lives
entirely in the key — a release body has no `name`/`namespace` fields.

#### `labels`

Free-form key/value labels used for selection (`-l`) and for label-based
`needs`. Values are coerced to strings, so `common: true` and `replicas: 3` are
accepted. Keys and values must be valid Kubernetes labels.

#### `chart`

```yaml
chart:
  name: bitnami/postgresql       # <repo-alias>/<chart>
  version: 15.x
```

`name` is required and may be:

- `alias/chart` — resolved against a declared helm repository;
- `oci://host/path/chart` — an OCI reference;
- anything else — a local chart path.

nelmwave orchestrates external charts only; it ships no chart templates of its
own.

#### `values` and `stores`

Both take a list of file references resolved through the datasource layer.
`values` become the release's values files (merged by nelm in order, Helm-style);
`stores` are companion files copied into the plan for anything else you need
alongside it.

Four equivalent spellings are accepted:

```yaml
values:
  - src: file://values/pg.yml.tpl   # mapping, with scheme
  - file://values/pg.yml.tpl        # bare string, with scheme
  - src: values/pg.yml.tpl          # mapping, no scheme (local file)
  - values/pg.yml.tpl               # bare string, no scheme (local file)
```

| Field | Meaning |
|---|---|
| `src` | Local path, or a URL with any gomplate datasource scheme (`env:`, `http(s)://`, `s3://`, `git://`, `vault://`, ...). |
| `name` | Names the resolved artifact under `.nelmwave/`. Default: an index-prefixed basename (`00-pg.yml`). |
| `optional` | A missing source is skipped instead of failing. |
| `strict` | Fail loudly on any resolution warning. |

**Behaviour is chosen by extension**, not by scheme:

| Extension | Behaviour |
|---|---|
| `.yml` / `.yaml` | copied verbatim |
| `.yml.tpl` | rendered through gomplate (`[[ ]]`) |
| `.yml.sops` | reserved — currently an explicit "not supported yet" error |

**Cross-references.** Within a release, `stores` resolve first, then `values`.
Each resolved artifact is registered as a gomplate datasource named
`stores/<artifact>` or `values/<artifact>`, where `<artifact>` is the resolved
file name — the `name` you gave it, or the generated `00-base.yml` otherwise. A
later `.tpl` can then pull in an earlier artifact:

```yaml
values:
  - { src: values/base.yml,     name: base.yml }
  - { src: values/app.yml.tpl,  name: app.yml }   # can read base.yml
```

```gotemplate
[[ (ds "values/base.yml").image.registry ]]
[[ include "stores/netpol.yml" ]]
```

Naming artifacts explicitly is worth it here: it keeps the datasource key stable
when you reorder the list.

Resolution is backward-only: an entry sees only artifacts resolved before it,
within the same release. A skipped `optional` source resolves to an empty
placeholder rather than an error. See
[`examples/datasources`](./examples/datasources).

#### `sets`

Inline value overrides, applied on top of `values` (highest precedence):

```yaml
sets:
  replicaCount: 3
  image.tag: "1.4.2"
  ingress.enabled: false
```

Keys are dotted paths, as with `helm --set`, but values keep their YAML type —
they reach nelm as type-preserving JSON, so `3` stays a number and `false` stays
a boolean.

#### `needs`

Dependency edges. All parts combine: a release waits for every release named in
`needs.releases` **plus** every release matched by the inlined label selector.

```yaml
needs:
  releases:
    postgres@data: { strict: true }
  matchLabels:
    tier: db
  matchLabelsExpressions:
    - { key: env, operator: In, values: [prod, stg] }
```

`strict` decides what happens when the dependency is filtered out of the current
selection: a strict need is an error, a non-strict one is dropped with a warning.
`up --include-needs` pulls filtered-out dependencies back into the run.

An empty label selector adds no dependencies — it does **not** match everything.
Cycles are rejected at build time, with the cycle printed.

#### `options`

nelm options that make sense per release:

| Field | Default | Meaning |
|---|---|---|
| `timeout` | none | Bounds the operation, e.g. `5m`. |
| `createNamespace` | `true` | Create the namespace if missing. |
| `autoRollback` | `false` | Roll back to the last deployed revision on failure (Helm's `--atomic`). |

### `Release:` — defaults for every release

The top-level `Release:` block is a confijer *type default*: it applies to every
value of the Go type `Release`, i.e. to every entry under `releases:`.

```yaml
Release:
  labels:
    common: true
  options:
    autoRollback: true
```

Maps deep-merge, and a release's own key wins:

```yaml
Release:
  labels: { team: platform, env: prod }

releases:
  api@app:
    labels: { team: data }     # -> {team: data, env: prod}
```

**Lists are replaced, not merged.** A release that declares its own `values:`
does not inherit `Release: { values: [...] }` — YAML semantics, deliberately kept.
To share a base values file, list it explicitly:

```yaml
releases:
  api@app:
    values:
      - values/common.yml
      - values/api.yml.tpl
```

---

## What `build` produces

```
.nelmwave/
  planfile.yml              resolved plan: releases, dependency edges, artifacts
  values/<uniqname>/...     values files, in merge order
  stores/<uniqname>/...     companion files from stores:
```

Values and store artifacts are rebuilt from scratch on every run, so sources
removed from the manifest leave nothing behind. The planfile is deterministic
(map keys are sorted), so it diffs cleanly between builds and is worth reading
in review.

---

## Commands

| Command | Purpose |
|---|---|
| `nelmwave build` | Render the manifest and write the plan to `.nelmwave/` |
| `nelmwave up` | Deploy the selected releases in dependency order |
| `nelmwave down` | Uninstall the selected releases in reverse order |
| `nelmwave diff` | Show the changes that would be applied (alias: `plan`) |

Global flags: `--log-level` (debug/info/warn/error), `--log-format`
(auto/console/json), `--kube-context`, `--kube-config`, `--version`.

Common command flags: `-l/--selector`, `--concurrency`, `--output`,
`--file` (build, `up --build`), `--include-needs` and `--dry-run` (up),
`--detailed-exitcode` (diff).

`--log-format auto` picks console output on a TTY and JSON everywhere else, so
CI logs stay machine-readable without a flag.

### Selecting releases

Selection uses Kubernetes-style label selectors:

```sh
nelmwave up -l 'app=api,env in (prod,stg),tier!=db'
```

Independent releases are applied in parallel; `--concurrency` bounds how many run
at once. A failure stops that branch of the graph — dependents are skipped,
unrelated branches keep going. `down` reverses the edges, so dependents are
removed before what they depend on.

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success (and, with `diff --detailed-exitcode`, no pending changes) |
| `1` | Something failed |
| `2` | `diff --detailed-exitcode` only: changes are planned |

---

## Not supported

- **SOPS** (`.yml.sops`) — reserved and rejected with an explicit error.
- **A built-in universal chart** — deliberately out of scope: nelmwave
  orchestrates external charts and ships no templates of its own.
- **Resource-level ordering annotations** (`werf.io/*`) for third-party charts —
  nelm's public API has no post-render hook, so only release-wide annotations are
  reachable.

---

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

> **Note on confijer:** the manifest loader binds keys via `json` struct tags
> (case-insensitively), not `yaml` tags. Config structs therefore carry both
> `json` (for loading) and `yaml` (for plan serialization) tags in sync.
