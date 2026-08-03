# Quickstart nelmwave manifest, rendered by gomplate (double-square-bracket delimiters).
project: my-platform

registries:
  - host: registry.example.com
    username: [[ getenv "REGISTRY_USER" "anonymous" ]]
    password: [[ getenv "REGISTRY_PASS" "" ]]

repositories:
  - name: bitnami
    url: https://charts.bitnami.com/bitnami
    force_update: true

releases:
  - name: postgres
    namespace: data
    labels:
      app: postgres
      tier: db
      env: [[ getenv "ENV" "prod" ]]
    chart:
      ref: bitnami/postgresql
      version: 15.x
    values:
      - src: file://values/pg.yml.tpl

  - name: api
    namespace: app
    labels:
      app: api
      tier: backend
      env: [[ getenv "ENV" "prod" ]]
    needs:
      - postgres
    chart:
      ref: oci://registry.example.com/charts/api
      version: 1.4.2
    values:
      - src: file://values/api.yml.tpl
    store:
      - src: file://extra/netpol.yml
        dst: manifests/netpol.yml

  - name: cache
    namespace: app
    labels: { app: redis, tier: cache, env: prod }
    universal:
      image: redis:7
      service:
        port: 6379
