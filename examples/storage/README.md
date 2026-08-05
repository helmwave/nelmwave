# Example: where release state lives

```sh
nelmwave build
cat .nelmwave/planfile.yml
```

Release state is the history of revisions: their manifests, values and status.
nelm picks the storage by a bare driver name; nelmwave takes a URL instead, so one
field carries the choice *and* its parameters — the same trick `repositories`
uses to tell a helm repo from an OCI registry.

| `driverURL` | State lives in |
|---|---|
| `kubernetes://secrets` | a Secret per revision, in the release namespace. **Default** |
| `kubernetes://configmaps` | a ConfigMap per revision |
| `psql://user@host:5432/db` | PostgreSQL (`postgres://` and `postgresql://` work too) |

An unknown value is rejected at build time — nelm panics on a driver name it does
not know rather than returning an error.

## Set it once

Put it in the `Release:` block, not per release. Different releases in different
stores is legal and almost never what you want: you then have to remember which
release lives where, and `nelmwave down` from a fresh checkout will not.

## Changing it later is not a migration

The old revisions stay where they were. nelmwave finds no history, treats the
release as new, and refuses to touch resources that still name the previous
release — or, with `forceAdoption`, adopts them and drops the history on the
floor. Move the state yourself before switching.

## configmaps

Compatibility with what Helm 2 did. The permission difference is the thing to
weigh: release state includes rendered values, and a ConfigMap is readable by
anyone who can `get configmaps` — including values you took the trouble to
encrypt with sops.

## PostgreSQL

Worth it when releases outgrow the ~1 MB an object can hold (large CRDs do this),
when history should outlive the namespace, or to keep the churn out of etcd. nelm
creates its own schema on first connect. Only PostgreSQL is supported.

**Do not put the password in the URL.** `build` copies the manifest into
`.nelmwave/planfile.yml`, so it would sit in cleartext on disk and in whatever
your CI keeps as an artifact. Leave it out and let libpq read the environment:

```sh
export PGPASSWORD=...
nelmwave up
```

`build` warns for every release whose `driverURL` embeds one:

```
WARN  driverURL embeds a password; it will be written to the planfile in
      cleartext (pass it via PGPASSWORD instead)  {"release": "huge@app"}
```

## No `memory://`

nelm has a memory driver; nelmwave does not offer it. State that dies with the
process cannot work across invocations: the next `up` would find no history,
treat the release as new, and try to adopt the resources it installed itself.
