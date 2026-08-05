# How a release treats the resources it owns, and what metadata it carries.
project: policies

Release:
  # Provenance of every rollout, stored with each revision of every release.
  # Read back with `nelm release get`; a map, so a release adds to these.
  annotations:
    ci/pipeline: [[ getenv "CI_PIPELINE_URL" "local" ]]
    ci/commit: [[ getenv "CI_COMMIT_SHA" "dirty" ]]
  labels:
    managed-by: nelmwave

releases:
  # Defaults, spelled out. Both policies are stated positively here; nelm spells
  # them as negations (NoRemoveManualChanges, NoInstallCRDs).
  api@app:
    labels: { app: api }
    chart: { name: ../charts/stub }
    autoRollback: true             # roll back on failure (helm --atomic)
    timeout: 5m
    removeManualChanges: true      # default: reclaim fields added by kubectl edit
    installCRDs: true              # default: install the chart's crds/
    deletePropagation: Foreground  # default; Background or Orphan also accepted
    historyLimit: 10               # default

  # Taking over resources from whatever managed them before. Only for migrations
  # and release renames: left on permanently, the next name collision silently
  # steals someone else's resources instead of failing.
  legacy@app:
    labels: { app: legacy }
    chart: { name: ../charts/stub }
    forceAdoption: true
    # ... and whose hand-made tweaks must survive the takeover.
    removeManualChanges: false

  # A release whose CRDs another pipeline owns, with a short history and
  # resources that outlive it.
  operator@platform:
    labels: { app: operator }
    chart: { name: ../charts/stub }
    installCRDs: false
    historyLimit: 3
    deletePropagation: Orphan      # `down` leaves dependent objects behind

  # Per-release metadata on top of the common set above (maps deep-merge).
  worker@app:
    labels: { app: worker, tier: batch }
    annotations:
      runbook: https://wiki.example.com/runbooks/worker
    chart: { name: ../charts/stub }
