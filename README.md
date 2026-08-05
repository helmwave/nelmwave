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

[`demo/`](./demo) holds an asciicast of the full loop against a throwaway
cluster — `asciinema play demo/nelmwave.cast`.

[`examples/`](./examples) has a runnable project per feature area — dependencies,
chart sources, namespaces, resource policies, release storage, sops-encrypted
values, datasource cross-references, and running in CI. `make examples` builds
them all.

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
        postgres@data: {}
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

A map keyed by alias (helm repos) or host (OCI registries). The URL scheme says
what it is and how to reach it: `https://` (or `http://`) is a classic helm
repository, `oci://` an OCI registry over TLS, `oci+http://` one without. A
value is either a bare URL string or an object:

| Field | Meaning |
|---|---|
| `url` | Repository index URL or OCI registry URL. Required. |
| `username`, `password` | Basic-auth credentials. |
| `insecureSkipTLSVerify` | Disable TLS verification for this repo. |
| `passCredentials` | Forward credentials to all domains, not just the repo host. |
| `caFile` | Path to a CA bundle for this repo. |
| `certFile`, `keyFile` | Client TLS certificate and key (mTLS to the repository). |
| — | Plain-HTTP OCI has no field: write the registry as `oci+http://` (see below). |
| `skipUpdate` | Don't refresh the chart's declared `dependencies:` before pulling them. No effect on charts without subcharts. |
| `requestTimeout` | Bound a single request to the repository, e.g. `30s`. The release `timeout` still applies on top. |
| `provenanceStrategy` | Verify the chart's PGP signature: `never` (default), `if-possible`, `always`, `later`. |
| `provenanceKeyring` | Keyring with the public keys to check the signature against. Defaults to helm's `~/.gnupg/pubring.gpg`. |

There is no `repositories.yaml` step: a helm-repo chart is fetched helm
`--repo` style (chart name plus repo URL), and OCI credentials are handed to nelm
through a generated, temporary `config.json`.

**Registries without TLS.** `oci://` names an artifact but not a transport, so
the client defaults to HTTPS. Write `oci+http://` to say otherwise:

```yaml
repositories:
  dev: oci+http://registry:5000       # local registry, no TLS
```

nelm accepts only `oci://`, so nelmwave rewrites the reference and passes the
choice along separately — you never write the scheme twice. The chart may be
addressed either way: `oci://registry:5000/api` resolves against the
`oci+http://` registry declared above, because the scheme is transport, not
identity. Spelling the chart itself `oci+http://…` works too, and is the only
option for a registry that is not declared at all.

This is unrelated to `insecureSkipTLSVerify`, which keeps TLS and only stops
verifying the certificate.

**Chart signatures.** A signed chart is published with a `.prov` file next to the
archive, holding a hash of it plus a PGP signature. `provenanceStrategy: always`
refuses to deploy a chart whose signature is missing or does not verify;
`if-possible` checks it only when a `.prov` exists. Most public repositories
publish no signatures at all, so `always` on `bitnami/*` will simply fail —
it is meant for internal repositories whose charts you pack and sign yourself.
The setting belongs to the repository, and it applies to OCI registries too:
an OCI chart is matched to its registry by address prefix, longest first.

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

They are also written onto the release's storage object (the Secret or ConfigMap
holding release state), so the same labels find the release in the cluster:

```sh
kubectl get secret -n app -l app=api,owner=helm
```

There is no second field for this. Helm's own `name`, `owner`, `status` and
`version` are applied after yours and win, so a label called `name` still selects
in the manifest but does not reach the storage object.

#### `annotations`

Stored with **each revision** of the release and read back with `nelm release
get` — the natural place for where a rollout came from:

```yaml
Release:                                        # once, for every release
  annotations:
    ci/pipeline: [[ getenv "CI_PIPELINE_URL" ]]
    ci/commit: [[ getenv "CI_COMMIT_SHA" ]]
```

Unlike `labels` these are **not selectable**: they live inside the serialized
release, not in the storage object's metadata, so `kubectl get -l` cannot see
them. In exchange they take values a label cannot — URLs, e-mail addresses,
commit messages. Values are coerced to strings, and being a map they deep-merge
with the `Release:` block, so per-release annotations add to the common ones
rather than replacing them.

These describe the release itself. Annotations on every rendered resource are a
different thing and not implemented yet.

#### `chart`

```yaml
chart:
  name: bitnami/postgresql       # <repo-alias>/<chart>
  version: 15.x
```

`name` is required and may be:

- `alias/chart` — resolved against a declared helm repository;
- `oci://host/path/chart` — an OCI reference (`oci+http://` for a registry
  without TLS);
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
| `optional` | A source that does not exist is skipped instead of failing the build. A source that exists but errors still fails. |

**Behaviour is chosen by extension**, not by scheme:

| Extension | Behaviour |
|---|---|
| `.yml` / `.yaml` | copied verbatim |
| `.yml.tpl` | rendered through gomplate (`[[ ]]`) |
| `.yml.sops` | decrypted with [sops](https://github.com/getsops/sops) |
| `.yml.tpl.sops` | decrypted, then rendered |

##### Encrypted sources

`.sops` sources are decrypted in-process — the `sops` binary is not required.
Keys come from the ambient environment, exactly as they do for the sops CLI:
`SOPS_AGE_KEY_FILE` / `SOPS_AGE_KEY`, GnuPG, or cloud KMS credentials. nelmwave
neither stores nor configures key material.

```yaml
values:
  - values/db-credentials.yml.sops
stores:
  - { src: secrets/tls.yml.sops, name: tls.yml }
```

The format handed to sops comes from the extension under `.sops`: `.yml`/`.yaml`
→ yaml, `.json` → json, `.env` → dotenv, anything else → binary.

**Encrypt templates as binary.** gomplate's `[[ ... ]]` is a valid YAML flow
sequence, so encrypting a template with `--input-type yaml` lets sops reshape
the actions into nested lists and silently destroy them:

```sh
sops --encrypt --input-type binary --output-type binary \
  --age "$AGE_RECIPIENT" secrets.yml.tpl > secrets.yml.tpl.sops
```

Plain (non-template) documents encrypt normally, and keep sops' per-value
encryption, which diffs and merges far better than an opaque blob:

```sh
sops --encrypt --age "$AGE_RECIPIENT" db-credentials.yml > db-credentials.yml.sops
```

Decrypted content is written to `.nelmwave/` in cleartext — it is a build
artifact directory, already `.gitignore`d, and should be treated as sensitive.
`build` says so out loud whenever a run decrypted anything:

```
WARN  decrypted secrets written in cleartext  {"sources": 1, "dir": ".nelmwave",
      "hint": "treat this directory as sensitive: do not publish it as a build artifact"}
```

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
    postgres@data: {}                 # required (the default)
    metrics@obs: { optional: true }    # nice to have
  matchLabels:
    tier: db
  matchLabelsExpressions:
    - { key: env, operator: In, values: [prod, stg] }
```

`optional` decides what happens when the dependency is filtered out of the
current selection. **A declared dependency is required by default**: selecting
the dependent release without it is an error. Marking it `optional: true` drops
the edge with a warning instead. [`up --include-needs`](#--include-needs) pulls
filtered-out dependencies back into the run either way.

Label-matched dependencies are always optional — a selector casts a wide net,
and failing because it happened to catch a filtered-out release would be
surprising. When a release is named explicitly *and* matched by the selector,
the explicit entry decides.

An empty label selector adds no dependencies — it does **not** match everything.
Cycles are rejected at build time, with the cycle printed.

#### `namespace`

Settings for the release's namespace. Not *which* namespace — that is part of
the release key (`api@production`) — but whether nelmwave creates it and what
metadata it carries:

```yaml
releases:
  api@production:
    chart: { name: repo/api }
    namespace:
      create: true
      labels:
        pod-security.kubernetes.io/enforce: restricted
        istio-injection: enabled
      annotations:
        owner: platform-team
```

| Field | Default | Meaning |
|---|---|---|
| `create` | `true` | Ensure the namespace exists before applying. |
| `delete` | `false` | Delete the namespace after `down` removes the release. |
| `labels` | none | Labels merged onto the namespace object. |
| `annotations` | none | Annotations merged onto the namespace object. |

Labels and annotations **merge**: keys nelmwave does not declare are left alone,
so it coexists with whatever else manages that namespace.

`delete` is not the mirror of `create`, which is why it defaults to `false`
while `create` defaults to `true`. The namespace is not owned by the release:
deleting it removes everything else living there — other releases, secrets,
PVCs — not just what nelmwave put in. `down` logs a warning for every release
that carries it, before uninstalling.

They are applied **before** the release, not after — a policy label such as
`istio-injection` or `pod-security.kubernetes.io/enforce` only affects workloads
created once it is in place. nelm's own API creates namespaces with nothing but
a name, so nelmwave writes this metadata itself.

With `create: false` and metadata declared, the namespace must already exist;
nelmwave patches it rather than creating one behind your back.

> Writing `namespace: production` (a string) is an error, not a silent no-op:
> the name belongs in the release key.

#### `timeout` and `autoRollback`

| Field | Default | Meaning |
|---|---|---|
| `timeout` | none | Bounds the operation, e.g. `5m`. |
| `autoRollback` | `false` | Roll back to the last deployed revision on failure (Helm's `--atomic`). |

#### Resource policies

How the release treats the resources it owns:

| Field | Default | Meaning |
|---|---|---|
| `forceAdoption` | `false` | Take over a resource that another Helm release claims through `meta.helm.sh/release-name`. Without it nelm refuses to touch it. |
| `removeManualChanges` | `true` | Reclaim fields added to a resource by hand (`kubectl edit`) that the manifest does not mention. Set to `false` to leave them alone. |
| `installCRDs` | `true` | Install the CRDs from the chart's `crds/` directory. Turn off where a separate pipeline owns CRDs. |
| `deletePropagation` | `Foreground` | Default deletion strategy: `Foreground`, `Background` or `Orphan`. Case-sensitive, and validated at build time. A single resource can override it with `werf.io/delete-propagation`. |
| `historyLimit` | `10` | How many revisions of the release to keep in storage. |

```yaml
releases:
  legacy@prod:
    chart: { name: repo/legacy }
    forceAdoption: true          # adopting resources from a previous tool
    removeManualChanges: false   # ... whose manual tweaks must survive
    historyLimit: 3
```

`forceAdoption` is for migrations and release renames — a rename makes the
release a new owner of existing resources. Leaving it on permanently means the
next name collision silently steals someone else's resources instead of failing.

`deletePropagation` and `historyLimit` apply to `down` as well as `up`;
`removeManualChanges` applies to both and to `diff`, so the preview matches what
the apply will do.

#### `driverURL`

Where the release keeps its state — the history of revisions, their manifests
and values. A URL, so one field carries both the choice and its parameters:

| URL | State lives in |
|---|---|
| `kubernetes://secrets` | A Secret per revision in the release namespace. The default. |
| `kubernetes://configmaps` | A ConfigMap per revision. |
| `psql://user@host:5432/db` | PostgreSQL. `postgres://` and `postgresql://` work too. |

```yaml
Release:                                   # once, for the whole manifest
  driverURL: psql://nelm@db.internal/nelm
```

Set it in the `Release:` block, not per release: a manifest whose releases keep
state in different places is a good way to lose one.

**Changing it later is not a migration.** The old revisions stay where they
were, so nelmwave finds no history, treats the release as new, and refuses to
touch resources that still name the previous release — or, with
`forceAdoption`, adopts them and drops the history on the floor. Move the state
yourself before switching.

`kubernetes://configmaps` exists for compatibility with what Helm 2 did. Mind
the permissions: release state includes rendered values, and a ConfigMap is
readable by anyone who can `get configmaps` — including the values you took the
trouble to encrypt with sops.

PostgreSQL is worth it when releases outgrow the ~1 MB an object can hold (large
CRDs do this), when history should outlive the namespace, or to keep the churn
out of etcd. nelm creates its own schema on first connect. **Do not put the
password in the URL**: `build` copies the manifest into
`.nelmwave/planfile.yml`, so it would sit in cleartext on disk and in CI
artifacts. Leave it out and let libpq read `PGPASSWORD` — `build` warns if it
finds one embedded.

nelm also has a `memory` driver. nelmwave does not offer it: state that dies
with the process means the next `up` sees no history and tries to adopt the
resources it installed itself.

### `Release:` — defaults for every release

The top-level `Release:` block is a confijer *type default*: it applies to every
value of the Go type `Release`, i.e. to every entry under `releases:`.

```yaml
Release:
  labels:
    common: true
  autoRollback: true
  namespace:
    labels:
      managed-by: nelmwave
```

The same works per type: a top-level `Namespace:` block applies its creation
policy and metadata to every release's namespace.

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
| `nelmwave completion` | Print a shell completion script (bash/zsh/fish/powershell) |

Global flags: `--log-level` (debug/info/warn/error), `--log-format`
(auto/console/json), `--version`, and the cluster-connection flags below
(`--kube-context`, `--kube-config`, ...).

Common command flags: `-l/--selector`, `--concurrency`, `--output`,
`--include-needs` (up, down, diff), `--file` (build, `up --build`),
`--dry-run` (up), `--detailed-exitcode` (diff).

`--log-format auto` picks console output on a TTY and JSON everywhere else, so
CI logs stay machine-readable without a flag.

`--log-level` also sets nelm's own verbosity, so `--log-level debug` gets you
the engine's debug output too, and `--log-level error` silences its progress.

### Shell completion

```sh
# bash — for the current shell
source <(nelmwave completion bash)

# bash — permanently (requires bash-completion)
nelmwave completion bash > /usr/local/etc/bash_completion.d/nelmwave   # macOS, brew
nelmwave completion bash > /etc/bash_completion.d/nelmwave             # Linux

# zsh — permanently, into a directory on your $fpath
nelmwave completion zsh > "${fpath[1]}/_nelmwave"
```

zsh needs `compinit` enabled; if it is not, add `autoload -U compinit; compinit`
to `~/.zshrc` before the line above. `fish` and `powershell` scripts are
available from the same command.

Beyond command and flag names, completion fills in values:

| Typing | Suggests |
|---|---|
| `-l <TAB>` | label keys from the built plan (`app=`, `tier=`, ...) |
| `-l app=<TAB>` | the values that key has across your releases |
| `-l app=api,<TAB>` | keys again, keeping what you already typed |
| `--kube-context <TAB>` | contexts from your kubeconfig |
| `--log-level <TAB>`, `--log-format <TAB>` | the accepted values |
| `--file <TAB>`, `--output <TAB>` | manifests (`.yml`/`.yaml`/`.tpl`) and directories |

Label completion reads `.nelmwave/planfile.yml` (or `--output`), so it starts
working after the first `build` and reflects the plan you are about to apply. The
commands take no positional arguments, so `nelmwave up <TAB>` offers nothing
instead of listing the current directory.

### Reaching the cluster

By default nelmwave reads a kubeconfig, exactly as `kubectl` would: `$KUBECONFIG`
when it is set, `~/.kube/config` otherwise. `--kube-config` overrides that and is
repeatable, behaving like `KUBECONFIG=a:b` — files merge, and where they disagree
the earlier one wins. A release's uniqname picks the context
(`api@app@staging`), and `--kube-context` sets it for releases that name none.
Every command resolves the connection the same way, `down` included.

Where there is no kubeconfig — CI with a ServiceAccount token — the connection
can be given directly:

```sh
nelmwave up \
  --kube-api-server https://k8s.example.com:6443 \
  --kube-token-path /var/run/secrets/kubernetes.io/serviceaccount/token \
  --kube-ca /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

| Flags | For |
|---|---|
| `--kube-config`, `--kube-config-base64`, `--kube-context`, `--kube-context-cluster`, `--kube-context-user` | Choosing what to use from a kubeconfig |
| `--kube-api-server`, `--kube-token`, `--kube-token-path`, `--kube-ca`, `--kube-ca-data`, `--no-verify-kube-tls`, `--kube-api-server-tls-name`, `--kube-proxy-url` | Connecting without one |
| `--kube-cert`, `--kube-cert-data`, `--kube-key`, `--kube-key-data`, `--kube-auth-username`, `--kube-auth-password` | Client certificates and basic auth |
| `--kube-impersonate-user`, `--kube-impersonate-group`, `--kube-impersonate-uid` | Acting as someone else (`kubectl --as`) |
| `--kube-qps-limit`, `--kube-burst-limit`, `--kube-request-timeout` | Client-side throttling |

These are flags and not manifest fields on purpose: a token or a key written into
`nelmwave.yml` would be copied into `.nelmwave/planfile.yml` and travel with the
build artifacts. Pass secrets on the command line or point at a file.

The same connection is used for nelmwave's own calls — the namespace metadata it
applies before handing over to nelm — so labels and workloads cannot end up in
different clusters.

### How much `diff` prints

`diff` hides parts of a change by default, the same way nelm's CLI does. Each
kind of omission has its own switch:

| Flag | Effect |
|---|---|
| `--no-verbose-diffs` | Replace the manifest of a resource created or deleted outright with `<hidden verbose changes>`. On by default — this flag turns it off. |
| `--show-verbose-crd-diffs` | Print full CRD manifests too. Off by default: a CRD schema is long enough to bury everything else. |
| `--show-insignificant-diffs` | Keep `helm.sh/*` and `werf.io/*` annotations and `managedFields` in the comparison. Reach for this when a release reports changes you cannot see — the difference is in what was stripped, shown as `<hidden insignificant changes>`. |
| `--show-sensitive-diffs` | Print Secrets and `werf.io/sensitive` resources in the clear instead of `<hidden sensitive changes>`. Local debugging only: in CI this writes secrets to the job log. |
| `--diff-context-lines` | Unified-diff context size (default 3). |

`up --dry-run` takes no such flags and always plans with these defaults; use
`nelmwave diff` when you need to change the view.

### Selecting releases

Selection uses Kubernetes-style label selectors:

```sh
nelmwave up -l 'app=api,env in (prod,stg),tier!=db'
```

Independent releases are applied in parallel; `--concurrency` bounds how many run
at once. A failure stops that branch of the graph — dependents are skipped,
unrelated branches keep going. `down` reverses the edges, so dependents are
removed before what they depend on.

#### `--include-needs`

Widens the selection along the dependency graph — **in the direction the command
travels**, which is not the same direction for every command:

| Command | Pulls in | Example |
|---|---|---|
| `up` | what the selection depends on | `up -l 'app=api' --include-needs` also installs `postgres` |
| `diff` | same as `up`, so the preview matches the apply | `diff -l 'app=api' --include-needs` also plans `postgres` |
| `down` | what depends on the selection | `down -l 'app=postgres' --include-needs` also removes `api` |

The inversion for `down` is deliberate: a teardown that pulled in dependencies
would delete *more* than you selected and leave the survivors broken, while
pulling in dependents removes exactly the things that would otherwise be left
pointing at something gone.

Without the flag, `up` refuses to run when a required dependency is filtered out
(see [`needs`](#needs)), while `down` and `diff` simply act on what you selected.

Every run logs the final selection before touching anything, and anything the
selector did not name is called out as a warning:

```
INFO   uninstall selection          {"count": 3, "releases": ["api@app", "cache@app", "postgres@data"]}
WARN   pulled in by --include-needs {"count": 2, "releases": ["api@app", "cache@app"]}
```

### Exit codes

| Code | Meaning |
|---|---|
| `0` | Success (and, with `diff --detailed-exitcode`, no pending changes) |
| `1` | Something failed |
| `2` | `diff --detailed-exitcode` only: changes are planned |

---

## Tests

```sh
make test     # unit tests, no cluster needed
make lint     # golangci-lint, config pinned in .golangci.yml
make e2e      # end-to-end: start a cluster, run the suite, tear it down
```

The end-to-end suite ([`test/e2e`](./test/e2e)) drives the real command tree
against a real Kubernetes API: build, up, a clean diff, a drifting diff with
exit code 2, the upgrade that resolves it, a selective down, a full down, and
the needs policy. It installs a local chart from `testdata`, so nothing
is downloaded and every assertion is about nelmwave's own behaviour.

The cluster is a k3s container owned by docker-compose, which keeps the fixture
in one file. To iterate without restarting it:

```sh
make e2e-up      # start k3s, wait for its healthcheck
make e2e-test    # run the suite (repeatable)
make e2e-down    # remove the container and its volumes
```

The suite is behind the `e2e` build tag, so `go test ./...` never reaches for a
cluster.

> **Using podman?** Point compose at its socket first:
> `export DOCKER_HOST="unix://$(podman machine inspect --format '{{.ConnectionInfo.PodmanSocket.Path}}')"`.
> Rootless podman additionally needs the `cpuset` controller delegated —
> without it k3s exits with `failed to find cpuset cgroup (v2)`:
>
> ```sh
> podman machine ssh 'sudo sh -c "printf \"[Service]\nDelegate=memory pids cpu io cpuset\n\" \
>   > /etc/systemd/system/user@.service.d/delegate.conf"'
> podman machine stop && podman machine start
> ```
>
> The remaining rootless workarounds (masking `/dev/kmsg`, nesting the cgroup,
> `KubeletInUserNamespace`) are already part of `docker-compose.yml` and are
> no-ops under a rootful runtime.

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
