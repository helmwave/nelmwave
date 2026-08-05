# Example: resource policies and release metadata

```sh
CI_PIPELINE_URL=https://ci.example.com/42 nelmwave build
cat .nelmwave/planfile.yml
```

## Policies

| Field | Default | Meaning |
|---|---|---|
| `autoRollback` | `false` | roll back to the last deployed revision on failure (helm's `--atomic`) |
| `timeout` | none | bound the operation, e.g. `5m` |
| `forceAdoption` | `false` | take over a resource another Helm release claims through `meta.helm.sh/release-name` |
| `removeManualChanges` | `true` | reclaim fields added by hand (`kubectl edit`) that the manifest does not mention |
| `installCRDs` | `true` | install the CRDs from the chart's `crds/` directory |
| `deletePropagation` | `Foreground` | `Foreground`, `Background` or `Orphan`; a resource can override with `werf.io/delete-propagation` |
| `historyLimit` | `10` | revisions of the release kept in storage |

Two of these read inverted against nelm, which spells them as things it skips
(`NoRemoveManualChanges`, `NoInstallStandaloneCRDs`). nelmwave states what it
does, so both default to `true`.

`deletePropagation` is validated at build time and is **case-sensitive** —
`foreground` is rejected. Without that check the string would travel unvalidated
all the way to the API server.

Scope differs per field: `deletePropagation` and `historyLimit` apply to `down`
as well as `up`; `removeManualChanges` applies to both and to `diff`, so the
preview matches the apply.

### forceAdoption

For migrations and renames — a rename makes the release a new owner of existing
resources, and nelm refuses to touch resources claimed by another release. Turn
it off once the migration is done: permanently on, it converts a name collision
from an error into a silent theft of someone else's resources.

## Metadata: labels vs annotations

Both describe the release itself, not its resources, and both deep-merge with the
`Release:` block. What they can hold, and where they end up, differs:

| | `labels` | `annotations` |
|---|---|---|
| Stored on | the release storage object (Secret/ConfigMap) | inside each revision of the release |
| Selectable | yes — `-l` here, `kubectl get secret -l` in the cluster | no |
| Values | must be valid Kubernetes labels | anything: URLs, e-mail, commit messages |
| Read back | `kubectl get secret -l app=api,owner=helm` | `nelm release get` |

So `labels` do double duty: they select releases in the manifest *and* find them
in the cluster. Helm's own `name`/`owner`/`status`/`version` are written after
yours and win, so a label called `name` still selects here but never reaches the
storage object.

`annotations` are per **revision**, which makes them the natural place for where a
rollout came from — pipeline URL, commit, who ran it. `historyLimit` revisions
back, you can still see which pipeline shipped what.
