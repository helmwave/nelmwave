# Example: namespaces

```sh
nelmwave build
cat .nelmwave/planfile.yml
```

The namespace **name** is part of the release key — `api@production` — and never a
field. The `namespace:` block is about everything else:

| Field | Default | Meaning |
|---|---|---|
| `create` | `true` | ensure the namespace exists before applying |
| `delete` | `false` | remove the namespace after `down` removes the release |
| `labels` | none | merged onto the namespace object |
| `annotations` | none | merged onto the namespace object |

## Why nelmwave writes this itself

nelm's API creates a namespace with nothing but a name — there is no hook for
metadata. So nelmwave talks to the cluster directly, and does it **before**
handing over to nelm: a label like `pod-security.kubernetes.io/enforce` or
`istio-injection` only affects workloads created once it is in place. After the
fact it would apply to nothing.

Metadata **merges**: keys nelmwave does not declare are left alone, so it
coexists with whatever else manages that namespace. With `create: false` the
namespace must already exist — nelmwave patches it rather than conjuring one.

## delete is not the mirror of create

`create` defaults to true, `delete` to false, and that asymmetry is deliberate:
the namespace is not owned by the release. Deleting it removes everything else
living there — other releases, secrets, PVCs — not just what nelmwave put in.
`down` logs a warning for every release that carries it, before uninstalling:

```
WARN  namespace will be deleted with the release, including anything else in it
      {"release": "preview-42@preview"}
```

Ephemeral preview environments are the case it is for.

## A scalar namespace is an error

```yaml
releases:
  api:
    namespace: production      # rejected
```

The name belongs in the key (`api@production`). This is an explicit error rather
than a silent no-op, because the config layer would otherwise drop the scalar and
the manifest would look like it worked.
