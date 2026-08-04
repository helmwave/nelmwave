# Examples

Each directory is a self-contained nelmwave project. Build any of them with:

```sh
cd <example>
nelmwave build
cat .nelmwave/planfile.yml
```

| Example | Shows |
|---|---|
| [`quickstart`](./quickstart) | The core schema: repositories (helm + OCI), releases by uniqname, needs (explicit + label selector), values/stores, inline `sets`, and per-type `Release:` defaults. |
| [`datasources`](./datasources) | Cross-referencing resolved artifacts: a values `.tpl` pulling an earlier values file (values -> values), plus value/store -> store, via `ds`/`include` (`values/<name>`, `stores/<name>`). |

> `build` renders and resolves everything locally into `.nelmwave/` — no cluster
> or chart download needed. `up`/`down`/`diff` need a Kubernetes cluster.
