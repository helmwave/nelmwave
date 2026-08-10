# Examples

Each directory is a self-contained nelmwave project, one per feature area. Build
any of them with:

```sh
cd <example>
nelmwave build
cat .nelmwave/planfile.yml
```

| Example | Shows |
|---|---|
| [`quickstart`](./quickstart) | The core schema: repositories (helm + OCI), releases by uniqname, needs, values/stores, inline `sets`, `Release:` defaults. |
| [`needs`](./needs) | Dependencies: explicit, by label selector, by expressions; `optional`, `--include-needs` in both directions, parallel apply. |
| [`labels`](./labels) | Labels and `-l` selection: `Release:` defaults, every selector operator, why `build` has no `-l`, completion. |
| [`repositories`](./repositories) | Every chart source: helm repo, OCI, `oci+http://`, basic auth, mTLS, chart signatures. |
| [`namespaces`](./namespaces) | The `namespace:` block: creation, deletion, policy labels applied before the release. |
| [`policies`](./policies) | Resource policies (`forceAdoption`, `removeManualChanges`, `installCRDs`, `deletePropagation`, `historyLimit`) and release `labels`/`annotations`. |
| [`storage`](./storage) | `driverURL`: release state in Secrets, ConfigMaps or PostgreSQL. |
| [`secrets`](./secrets) | sops-encrypted values, including an encrypted template. Runnable — the example age key is committed. |
| [`datasources`](./datasources) | Cross-referencing resolved artifacts through `ds`/`include` (`values/<name>`, `stores/<name>`). |
| [`ci`](./ci) | The command line rather than the manifest: connecting without a kubeconfig, gating on drift, staged rollouts, JSON logs. |

Every example builds without a cluster, a registry or a network: charts point at
the shared [`charts/stub`](./charts/stub), or at repositories that are only
recorded in the plan. `up`, `down` and `diff` need a real cluster.

`make examples` builds all of them at once, which is also how they are kept honest
as the schema changes.

## Shell completion

Worth setting up before exploring the rest:

```sh
source <(nelmwave completion bash)          # or: nelmwave completion zsh > "${fpath[1]}/_nelmwave"
```

Then, inside any example directory after a `build`:

```
nelmwave up -l <TAB>            → app=  env=  tier=
nelmwave up -l app=<TAB>        → app=api  app=postgres  app=worker
nelmwave --kube-context <TAB>   → the contexts in your kubeconfig
```

Label completion reads the built plan, so it always offers what this project
actually has.
