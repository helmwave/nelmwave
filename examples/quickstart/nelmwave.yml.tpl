# Quickstart nelmwave manifest, rendered by gomplate (double-square-bracket delimiters).
project: my-platform

# Maps keyed by identity: registries by host, repositories by name, releases by
# uniqname "name[@namespace[@kubecontext]]" (namespace/context optional — the
# current kube-context is used when omitted).
registries:
  registry.example.com:
    username: [[ getenv "REGISTRY_USER" "anonymous" ]]
    password: [[ getenv "REGISTRY_PASS" "" ]]

repositories:
  bitnami:
    url: https://charts.bitnami.com/bitnami
    force_update: true

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
      - postgres@data
    chart:
      name: oci://registry.example.com/charts/api
      version: 1.4.2
    values:
      - src: values/api.yml.tpl
    store:
      - src: extra/netpol.yml
        dst: manifests/netpol.yml

  cache@app:
    labels: { app: redis, tier: cache, env: prod }
    universal:
      image: redis:7
      service:
        port: 6379
