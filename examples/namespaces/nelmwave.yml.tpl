# The namespace block: not *which* namespace (that is the release key), but
# whether nelmwave creates it, what metadata it carries, and whether `down`
# removes it.
project: namespaces

# A top-level block named after the Go type applies to every release's namespace.
# Maps deep-merge, so a release adds to these rather than replacing them.
Namespace:
  labels:
    managed-by: nelmwave

releases:
  # Policy labels are the reason this block exists: pod-security and
  # istio-injection only affect workloads created *after* they land, and nelm's
  # API creates namespaces with nothing but a name.
  api@production:
    labels: { app: api }
    chart: { name: ../charts/stub }
    namespace:
      create: true                 # default
      labels:
        pod-security.kubernetes.io/enforce: restricted
        istio-injection: enabled
      annotations:
        owner: platform-team

  # An existing namespace nelmwave must not create — it only patches metadata,
  # and fails if the namespace is missing.
  legacy@shared:
    labels: { app: legacy }
    chart: { name: ../charts/stub }
    namespace:
      create: false
      annotations:
        contact: legacy-team@example.com

  # A throwaway namespace: `down` takes it with the release.
  #
  # delete is NOT the mirror of create (which defaults to true) — the namespace
  # is not owned by the release, so deleting it removes everything else living
  # there too. `down` warns for every release carrying it.
  preview-42@preview:
    labels: { app: api, ephemeral: true }
    chart: { name: ../charts/stub }
    namespace:
      create: true
      delete: true

  # No namespace block at all: created if missing (create defaults to true),
  # and it still inherits the Namespace: labels above.
  worker@production:
    labels: { app: worker }
    chart: { name: ../charts/stub }
