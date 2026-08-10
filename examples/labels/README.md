# Example: labels and selection

One manifest, seven releases, and the label vocabulary that lets a command touch
any subset of them.

```sh
nelmwave build
nelmwave up   -l 'tier=backend'
nelmwave up   -l 'tier in (backend,frontend)'
nelmwave diff -l 'env=prod,!track'
nelmwave down -l 'env notin (prod)'
```

## build has no `-l`

Selection is a **runtime** concern. `build` always renders the whole manifest and
writes the whole plan; `up`, `down` and `diff` read that plan and filter it with
`-l`. One reviewed artifact, many subsets applied from it — and the subset you
deployed yesterday is still described by the plan you built yesterday.

```sh
nelmwave build                 # whole manifest, every time
nelmwave up -l 'app=api'       # a slice of the plan
nelmwave up --build -l 'app=api'   # rebuild first — still builds everything
```

## Writing labels

```yaml
Release:
  labels:                                        # defaults for every release
    managed-by: nelmwave
    commit: [[ getenv "CI_COMMIT_SHA" "dirty" ]] # templated like anything else
  chart: { name: ../charts/stub }
releases:
  api@app:
    labels: { app: api, tier: backend, critical: true }
```

Being a map, `Release.labels` **deep-merges**: `api@app` ends up with all five
keys, and a release may override a default by repeating the key. Values are
coerced to strings, so `true` and `3` are accepted as written — the plan stores
`critical: "true"`. Keys and values must be valid Kubernetes labels; anything
else fails at build time, not at apply time.

## Selecting with them

`-l` takes a Kubernetes label selector, so `=` is only the first of six
operators. Commas are **AND**; there is no OR — `in (...)` is how you spell it.

| Selector | Operator | Matches here |
|---|---|---|
| `app=api` | equality (`==` is the same operator) | `api@app`, `api@stg` |
| `tier!=db` | inequality | all six that are not the database |
| `tier in (backend,frontend)` | set membership | `api@app`, `api@stg`, `web@app`, `web-canary@app`, `worker@app` |
| `env notin (stg)` | set exclusion | the six prod releases |
| `critical` | key exists, any value | `api@app`, `postgres@data` |
| `!critical` | key absent | the other five |
| `critical=true` | equality against a coerced value | `api@app`, `postgres@data` |
| `env=prod,tier!=db` | AND | prod, minus the database |
| `team=payments,!track` | AND across two operators | `api@app`, `api@stg`, `web@app`, `worker@app` |
| `tier in (backend,frontend),!track,env=prod` | three terms | `api@app`, `web@app`, `worker@app` |

Two things about the negative operators are worth knowing before you rely on
them. **`!=` and `notin` also match a release that has no such key at all** —
`track notin (canary)` selects the six releases that never mentioned `track`,
and `critical!=true` selects the five with no `critical` label rather than only
those that set it to something else. And `critical=true` works at all only
because label values are coerced to strings at build time: the manifest says
`critical: true`, the plan stores `"true"`, and the selector compares strings.

`!key` is the reason `web-canary@app` carries a `track` label nobody else has: an
absent label is a first-class thing to select on, and it is usually cleaner than
adding `track: stable` to every other release.

The same six operators, as commands:

```sh
nelmwave up   -l 'app=api'                                # equality
nelmwave up   -l 'tier!=db'                               # inequality
nelmwave up   -l 'tier in (backend,frontend)'             # membership — mind the quotes, it has spaces
nelmwave down -l 'env notin (prod)'                       # exclusion — tear down everything but prod
nelmwave diff -l 'critical'                               # exists
nelmwave up   -l '!critical' --concurrency 4              # does not exist
nelmwave diff -l 'tier in (backend,frontend),!track'      # AND
```

An empty selector matches everything. A selector matching nothing is a warning
and a no-op, not an error:

```
warn  no releases match the selector  {"selector": "nosuch=1"}
```

## Labels cross the selection boundary

`-l` picks a set; the dependency graph can widen it. `web@app` needs `api@app`,
which needs `postgres@data`:

```sh
nelmwave up -l 'tier=frontend'                  # required need outside the selection -> error
nelmwave up -l 'tier=frontend' --include-needs  # api@app and postgres@data pulled in, applied first
```

`--include-needs` travels the direction the command does — `up`/`diff` pull in
what the selection depends on, `down` pulls in what depends on it:

```sh
nelmwave down -l 'tier=db' --include-needs
# also removes web@app, web-canary@app, api@app — the things that would be left
# talking to a database that is gone
```

Anything dragged in is logged as a warning, by name, before it is touched. See
[`needs`](../needs) for the full story.

## The same labels are also `needs`

A selector is not only something you type. `worker@app` declares its dependency
with one:

```yaml
releases:
  worker@app:
    chart: { name: ../charts/stub }
    needs:
      matchLabels: { tier: cache }
```

Add a second cache release and `worker@app` waits for it too, with no edit. Label
matched needs are always optional — a broad net should not fail your run.

## And in the cluster

Release labels are written onto the storage object holding release state, so the
same vocabulary finds the release with `kubectl`:

```sh
kubectl get secret -n app -l app=api,managed-by=nelmwave
kubectl get secret -A -l commit=1a2b3c4          # everything from one CI run
```

Helm's own `name`, `owner`, `status` and `version` are applied after yours and
win. Annotations are **not** selectable — see [`policies`](../policies) for the
difference.

## Completion

After a `build`, the shell completes selectors from the plan itself:

```
nelmwave up -l <TAB>              → app=  commit=  critical=  env=  managed-by=  team=  tier=  track=
nelmwave up -l app=<TAB>          → app=api  app=postgres  app=redis  app=web  app=worker
nelmwave up -l tier=backend,<TAB> → every key again, prefixed with what you typed
```

Completion only ever offers the `=` form, because that is the one it can build
mechanically from the plan's labels. The other five operators you type yourself.
