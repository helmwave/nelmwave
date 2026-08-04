# Manifest driving the end-to-end suite. Both releases install the same local
# chart from testdata/chart, so nothing is downloaded and the assertions are
# about nelmwave's behaviour, not about someone else's chart.
#
# The test supplies E2E_CHART (absolute path to the chart), E2E_NS (namespace)
# and E2E_MESSAGE (the value it later asserts on and changes).
project: nelmwave-e2e

# Type defaults reach every release: proof that the confijer bucket survives the
# whole build -> plan -> apply path, not just unit tests.
Release:
  labels:
    suite: e2e
  options:
    timeout: 3m

releases:
  # A dependency, selected by the label selector below rather than by name.
  base@[[ .Env.E2E_NS ]]:
    labels:
      app: base
      tier: data
    chart:
      name: [[ .Env.E2E_CHART ]]
    values:
      - values/base.yml
    sets:
      # Highest precedence: overrides values/base.yml.
      message: [[ .Env.E2E_MESSAGE ]]

  app@[[ .Env.E2E_NS ]]:
    labels:
      app: app
      tier: front
    needs:
      # Inline k8s selector: wait for every release labelled tier=data.
      matchLabels:
        tier: data
    chart:
      name: [[ .Env.E2E_CHART ]]
    values:
      - values/app.yml
