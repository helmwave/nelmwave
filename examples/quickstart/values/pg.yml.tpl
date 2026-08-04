auth:
  # rendered by gomplate at build time from the environment
  password: [[ getenv "PG_PASSWORD" "changeme" ]]
primary:
  persistence:
    size: [[ getenv "PG_SIZE" "8Gi" ]]
# override the global default (deep-merge, per-release wins)
resources:
  requests:
    cpu: 250m
