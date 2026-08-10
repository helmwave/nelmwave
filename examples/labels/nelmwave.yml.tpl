# Labels, and the selection they buy you: one manifest, many subsets of it.
#
#   nelmwave build                              # always the whole manifest, -l is not a build flag
#   nelmwave up   -l 'tier=backend'             # =   equality; selection happens against the built plan
#   nelmwave up   -l 'tier!=db'                 # !=  inequality (also matches a missing key)
#   nelmwave up   -l 'tier in (backend,cache)'  # in  set membership — there is no OR, this is it
#   nelmwave down -l 'env notin (prod)'         # notin  set exclusion (also matches a missing key)
#   nelmwave diff -l 'critical'                 # key exists, any value
#   nelmwave diff -l 'env=prod,!track'          # commas are AND; !key means the key is absent
project: labels

Release:
  # Labels on the Release: block are type defaults: a map, so they deep-merge
  # with each release's own labels instead of being replaced by them.
  labels:
    managed-by: nelmwave
    commit: [[ getenv "CI_COMMIT_SHA" "dirty" ]]   # a label may be templated
  chart: { name: ../charts/stub }

releases:
  # ---- data tier ----
  postgres@data:
    labels: { app: postgres, tier: db, env: prod, team: platform, critical: true }

  redis@data:
    # No `critical` key at all: that is what `-l '!critical'` selects on. An
    # absent label and a false one are different things to a selector.
    labels: { app: redis, tier: cache, env: prod, team: platform }

  # ---- backend ----
  api@app:
    # Values are coerced to strings, so `true` and `3` are accepted as written.
    labels: { app: api, tier: backend, env: prod, team: payments, critical: true }
    needs:
      releases:
        postgres@data: {}

  worker@app:
    labels: { app: worker, tier: backend, env: prod, team: payments }
    needs:
      # Labels are also how a release finds its dependencies, not just how you
      # find the release. Same vocabulary, two uses.
      matchLabels: { tier: cache }

  # ---- frontend ----
  web@app:
    labels: { app: web, tier: frontend, env: prod, team: payments }
    needs:
      releases:
        api@app: {}

  # Same app, same tier, one extra label. `track` exists only here, which makes
  # `-l '!track'` mean "everything except the canary" without touching the rest.
  web-canary@app:
    labels: { app: web, tier: frontend, env: prod, team: payments, track: canary }
    needs:
      releases:
        api@app:
          optional: true

  # ---- another environment in the same manifest ----
  # Identity is the map key, so api@app and api@stg coexist; the env label is
  # what tells them apart in a selector.
  api@stg:
    labels: { app: api, tier: backend, env: stg, team: payments }
