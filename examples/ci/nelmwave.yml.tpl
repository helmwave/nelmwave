# A manifest shaped for a pipeline: everything environment-specific arrives
# through the environment, so the same file builds for every stage.
project: ci

Release:
  labels:
    env: [[ getenv "ENV" "stg" ]]
  # Where this rollout came from, kept with each revision of every release.
  annotations:
    ci/pipeline: [[ getenv "CI_PIPELINE_URL" "local" ]]
    ci/commit: [[ getenv "CI_COMMIT_SHA" "dirty" ]]

releases:
  api@[[ getenv "ENV" "stg" ]]:
    labels: { app: api, tier: backend }
    chart: { name: ../charts/stub }
    timeout: 5m
    autoRollback: true
    sets:
      image.tag: [[ getenv "CI_COMMIT_SHORT_SHA" "latest" ]]

  worker@[[ getenv "ENV" "stg" ]]:
    labels: { app: worker, tier: batch }
    needs:
      matchLabels: { tier: backend }
    chart: { name: ../charts/stub }
