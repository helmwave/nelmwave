# Manifest for the --download-charts path, with one release per kind of chart:
#
#   downloaded — a *remote* chart from a helm repository the test starts itself
#                (E2E_REPO_URL) and then stops before `up`, so the release can
#                only come from what build put in the build directory;
#   copied     — the same chart as a local path, which build copies in.
#
# Both must deploy from .nelmwave/ alone.
project: nelmwave-e2e-charts

releases:
  downloaded@[[ .Env.E2E_NS ]]:
    labels:
      app: downloaded
    timeout: 3m
    chart:
      name: e2e/nelmwave-e2e
      version: 0.1.0
    sets:
      message: [[ .Env.E2E_MESSAGE ]]

  copied@[[ .Env.E2E_NS ]]:
    labels:
      app: copied
    timeout: 3m
    chart:
      name: [[ .Env.E2E_CHART ]]
    sets:
      message: [[ .Env.E2E_MESSAGE ]]

repositories:
  e2e: [[ .Env.E2E_REPO_URL ]]
