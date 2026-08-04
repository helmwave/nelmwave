[[- $base := ds "values/base.yml" -]]
# value -> value: read parsed fields from the earlier "base.yml" values file, so
# sizing is declared once there and only derived here.
autoscaling:
  enabled: true
  minReplicas: [[ $base.replicaCount ]]
  maxReplicas: [[ math.Mul $base.replicaCount 4 ]]

podAnnotations:
  app.example.com/image-tag: "[[ $base.image.tag ]]"
  app.example.com/cpu-limit: "[[ $base.resources.limits.cpu ]]"

# value -> store: pull a parsed field from a store...
namespaceOverride: [[ (ds "stores/network.yml").namespace ]]
commonAnnotations:
  net.example.com/cidr: "[[ (ds "stores/network.yml").ingressCIDR ]]"

# ...and embed a whole rendered manifest verbatim (raw include, indented for YAML).
extraDeploy:
  - |
[[ include "stores/netpol.yml" | strings.Indent 4 ]]
