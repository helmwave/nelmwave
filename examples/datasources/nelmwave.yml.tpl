# Example: cross-referencing resolved artifacts via gomplate datasources.
#
# Within a release, stores are resolved first (in order), then values. Each
# resolved artifact is registered as a datasource named "stores/<name>" or
# "values/<name>", so a later *.tpl artifact can pull an earlier one with
# `ds` (parsed) or `include` (raw). An item only sees artifacts resolved
# before it; values see all stores.
project: datasources-demo

repositories:
  bitnami: https://charts.bitnami.com/bitnami

releases:
  web@frontend:
    chart:
      name: bitnami/nginx
      version: 18.x

    stores:
      # 1. plain shared data (copied as-is)
      - { src: shared/network.yml, name: network.yml }
      # 2. a manifest rendered FROM the earlier store (store -> store)
      - { src: templates/netpol.yml.tpl, name: netpol.yml }

    values:
      # a value rendered FROM the stores (value -> store): a parsed field and a
      # whole embedded manifest.
      - src: values/app.yml.tpl
