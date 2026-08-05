# The project recorded in demo/nelmwave.cast: three releases, two namespaces,
# one local chart — so the whole build/diff/up/down loop runs against a
# throwaway cluster without downloading anything but registry.k8s.io/pause.
project: demo

Release:
  labels:
    demo: "true"
  chart:
    name: ./chart
  timeout: 2m

releases:
  # The dependency: nothing of its own, but everything below waits for it.
  postgres@demo-data:
    labels: { app: postgres, tier: db }
    values:
      - values/common.yml

  # Explicit need, by uniqname.
  api@demo-app:
    labels: { app: api, tier: backend }
    needs:
      releases:
        postgres@demo-data: {}
    values:
      - values/api.yml.tpl
    sets:
      # MSG travels: shell env -> gomplate -> plan -> chart -> ConfigMap.
      message: [[ getenv "MSG" "v1" ]]

  # Label-matched need: whatever carries tier=db becomes a dependency.
  cache@demo-app:
    labels: { app: redis, tier: cache }
    needs:
      matchLabels:
        tier: db
    values:
      - values/common.yml
