# Dependencies between releases: the three ways to declare them, and what
# happens when a dependency falls outside the selection.
project: needs

releases:
  # ---- the leaves: nothing depends on these ----
  postgres@data:
    labels: { app: postgres, tier: db, critical: true }
    chart: { name: ../charts/stub }

  redis@data:
    labels: { app: redis, tier: cache }
    chart: { name: ../charts/stub }

  vault@platform:
    labels: { app: vault, tier: secrets }
    chart: { name: ../charts/stub }

  # ---- 1. explicit needs, by uniqname ----
  api@app:
    labels: { app: api, tier: backend }
    needs:
      releases:
        postgres@data: {}          # required: must be in the selection
        redis@data:
          optional: true           # tolerated: dropped with a warning if filtered out
    chart: { name: ../charts/stub }

  # ---- 2. label selector: depend on whatever matches ----
  worker@app:
    labels: { app: worker, tier: backend }
    needs:
      # Every release carrying tier=db becomes a dependency. Label-matched needs
      # are always optional — a selector is a broad net, and a release it happens
      # to catch should not be able to fail the run.
      matchLabels:
        tier: db
    chart: { name: ../charts/stub }

  # ---- 3. selector with expressions, for anything matchLabels cannot say ----
  gateway@edge:
    labels: { app: gateway, tier: edge }
    needs:
      matchLabelsExpressions:
        - { key: tier, operator: In, values: [backend, secrets] }
        - { key: app, operator: NotIn, values: [worker] }
      # Both forms may be combined; the release then depends on the union of
      # explicit needs, matchLabels matches and expression matches.
      releases:
        postgres@data: {}
    chart: { name: ../charts/stub }
