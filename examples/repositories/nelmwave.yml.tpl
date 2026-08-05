# Chart sources: every kind of repository nelmwave can fetch from, and the
# settings each kind accepts. The URL scheme decides what a repository is and how
# to reach it.
project: repositories

repositories:
  # ---- classic helm repository, no auth: the short form is just the URL ----
  bitnami: https://charts.bitnami.com/bitnami

  # ---- helm repository behind basic auth ----
  internal:
    url: https://charts.corp.example.com
    username: [[ getenv "CHARTS_USER" "reader" ]]
    password: [[ getenv "CHARTS_PASS" "" ]]
    # Send the credentials to every host, not just this one. Needed when the
    # index points at downloads on another domain — and a good way to leak them
    # if that domain is not yours.
    passCredentials: false
    # Don't refresh the chart's declared dependencies before pulling them.
    # Only affects charts with a `dependencies:` section.
    skipUpdate: false
    # Give up on a single request to this repository. The release timeout still
    # applies on top.
    requestTimeout: 30s

  # ---- helm repository with a private CA and client certificates (mTLS) ----
  mtls:
    url: https://charts.secure.example.com
    caFile: [[ getenv "CORP_CA" "/etc/ssl/certs/ca-certificates.crt" ]]
    # caFile says whom we trust; these say who we are.
    certFile: [[ getenv "CLIENT_CERT" "" ]]
    keyFile: [[ getenv "CLIENT_KEY" "" ]]
    # Last resort: keep TLS but stop checking the certificate. Prefer caFile.
    insecureSkipTLSVerify: false

  # ---- OCI registry over TLS, with credentials ----
  ghcr.io:
    url: oci://ghcr.io
    username: [[ getenv "GITHUB_ACTOR" "" ]]
    password: [[ getenv "GITHUB_TOKEN" "" ]]

  # ---- OCI registry without TLS: the scheme says so ----
  # There is no separate "plain http" field — oci+http:// is the whole answer.
  dev: oci+http://registry:5000

  # ---- OCI registry whose charts are signed ----
  signed:
    url: oci://registry.corp.example.com
    # never (default) | if-possible | always | later
    provenanceStrategy: if-possible
    provenanceKeyring: [[ getenv "CHART_KEYRING" "" ]]

releases:
  # alias/chart -> resolved against the declared helm repository
  postgres@data:
    labels: { source: helm-repo }
    chart: { name: bitnami/postgresql, version: 15.x }

  # oci:// -> fetched by URL; the registry is matched by address, so it
  # contributes its credentials and TLS settings
  api@app:
    labels: { source: oci }
    chart: { name: oci://ghcr.io/acme/api, version: 1.4.2 }

  # A chart in the plain-HTTP registry above. Writing it as oci:// also works:
  # the scheme is transport, not identity, so both spellings match `dev`.
  sandbox@dev:
    labels: { source: oci-plain-http }
    chart: { name: oci+http://registry:5000/sandbox, version: 0.1.0 }

  # A local path: no repository involved at all.
  stub@app:
    labels: { source: local }
    chart: { name: ../charts/stub }
