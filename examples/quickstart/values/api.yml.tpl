replicaCount: [[ getenv "API_REPLICAS" "2" ]]
image:
  tag: [[ getenv "API_TAG" "1.4.2" ]]
