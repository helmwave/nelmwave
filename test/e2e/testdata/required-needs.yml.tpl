# Manifest for the needs-policy test: `dependent` declares a plain (required)
# need on `dependency`, so selecting only the dependent one must fail before
# anything reaches the cluster, and --include-needs must pull the dependency
# back in. Marking the need `optional: true` would turn that failure into a
# dropped edge instead.
project: nelmwave-e2e-needs

releases:
  dependency@[[ .Env.E2E_NS ]]:
    labels:
      app: dependency
    chart:
      name: [[ .Env.E2E_CHART ]]
    sets:
      message: dependency

  dependent@[[ .Env.E2E_NS ]]:
    labels:
      app: dependent
    needs:
      releases:
        # No `optional:` — a declared dependency is required by default.
        dependency@[[ .Env.E2E_NS ]]: {}
    chart:
      name: [[ .Env.E2E_CHART ]]
    sets:
      message: dependent
