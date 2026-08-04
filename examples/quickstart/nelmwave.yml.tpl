# Quickstart nelmwave manifest, rendered by gomplate (double-square-bracket delimiters).
project: my-platform

# Maps keyed by identity: repositories by alias/host, releases by uniqname
# "name[@namespace[@kubecontext]]" (namespace/context optional — the current
# kube-context is used when omitted).
#
# repositories cover both helm repos (https://) and OCI registries (oci://).
# A value may be a bare URL string, or a full object when auth/flags are needed.
repositories:
  bitnami: https://charts.bitnami.com/bitnami   # bare URL (short form)
  registry.example.com:                          # full form: OCI with creds
    url: oci://registry.example.com
    username: [[ getenv "REGISTRY_USER" "anonymous" ]]
    password: [[ getenv "REGISTRY_PASS" "" ]]

# Defaults for every release via confijer type-defaults ("Release" = the Go type).
# Labels deep-merge into each release (a release's own label wins); values act as
# a default used only when a release declares none of its own.
Release:
  labels:
    common: true
  values:
    - values/common.yml

releases:
  postgres@data:
    labels:
      app: postgres
      tier: db
      env: [[ getenv "ENV" "prod" ]]
    chart:
      name: bitnami/postgresql
      version: 15.x
    # values entries accept any of: {src: url}, bare url, with or without scheme.
    values:
      - values/pg.yml.tpl

  api@app:
    labels:
      app: api
      tier: backend
      env: [[ getenv "ENV" "prod" ]]
    needs:
      releases:
        postgres@data:
          strict: true          # fail if this dependency is filtered out
    chart:
      name: oci://registry.example.com/charts/api
      version: 1.4.2
    values:
      - src: values/api.yml.tpl
    # inline overrides (helm --set style), applied on top of values;
    # keys are dotted paths, values keep their YAML type.
    sets:
      replicaCount: 3
      image.tag: [[ getenv "API_TAG" "1.4.2" ]]
    store:
      - src: extra/netpol.yml
        dst: manifests/netpol.yml

  cache@app:
    labels: { app: redis, tier: cache, env: prod }
    needs:
      # Kubernetes-style label selector inlined into needs: wait for every
      # release it matches.
      matchLabels:
        tier: db
      # matchLabelsExpressions:
      #   - { key: env, operator: In, values: [prod, stg] }
    chart:
      name: bitnami/redis
      version: 20.x
