# Rendered by gomplate during build, exactly like the manifest.
message: overridden-by-sets
replicas: [[ getenv "API_REPLICAS" "2" ]]
