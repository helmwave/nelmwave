# Example: chart sources

Every kind of repository nelmwave can pull a chart from, and what each kind
accepts. There is no `repositories.yaml` step and no `repo add`: a helm-repo
chart is fetched helm `--repo` style, and OCI credentials are handed to nelm
through a generated, temporary `config.json`.

```sh
nelmwave build
cat .nelmwave/planfile.yml
```

`build` reaches no registry — it only records the plan. Charts are downloaded by
`up`/`diff`.

## The scheme decides everything

| URL | Kind |
|---|---|
| `https://charts.example.com` | helm repository |
| `http://charts.example.com` | helm repository, no TLS |
| `oci://registry.example.com` | OCI registry over TLS |
| `oci+http://registry:5000` | OCI registry without TLS |

`oci+http://` exists because `oci://` names an artifact but not a transport: the
registry client defaults to HTTPS and has no scheme to read otherwise. nelm itself
only understands `oci://`, so nelmwave rewrites the reference and passes the
choice along — you never write it twice.

**Not the same as `insecureSkipTLSVerify`:**

| | `insecureSkipTLSVerify` | `oci+http://` |
|---|---|---|
| TLS | happens, certificate unchecked | none at all |
| For | self-signed or private-CA registry | registry serving plain HTTP |

One will not do for the other: a plain-HTTP request into a TLS port gets garbage
back, and a TLS handshake with a plaintext server never completes. For a private
CA, `caFile` beats both.

## How a chart finds its repository

| `chart.name` | Resolution |
|---|---|
| `bitnami/postgresql` | the alias before `/` is looked up in `repositories` |
| `oci://ghcr.io/acme/api` | matched to a registry by **address prefix**, longest first |
| `./charts/stub` | local path, no repository |

Address, not URL: a chart written `oci://registry:5000/x` matches a registry
declared `oci+http://registry:5000`, since the scheme is transport rather than
identity. Nesting works too — `oci://ghcr.io/acme` wins over a broader
`oci://ghcr.io`. An `oci://` chart whose registry is not declared still works; it
just gets no credentials and no settings.

## Chart signatures

A signed chart is published with a `.prov` file beside the archive, holding a hash
of it plus a PGP signature.

| `provenanceStrategy` | Behaviour |
|---|---|
| `never` (default) | ignore signatures |
| `if-possible` | verify when a `.prov` exists |
| `always` | refuse a chart whose signature is missing or does not verify |
| `later` | download the `.prov` for verification elsewhere |

Public repositories mostly publish none, so `always` on `bitnami/*` simply fails —
this is for internal repositories whose charts you sign yourself. A wrong value is
rejected at build time, because helm's downloader panics on one instead of
returning an error.

## Credentials

Keep them out of the manifest text:

```yaml
username: [[ getenv "CHARTS_USER" ]]
password: [[ getenv "CHARTS_PASS" ]]
```

The rendered value still lands in `.nelmwave/planfile.yml`, so treat that
directory as a secret when your repositories need auth.
