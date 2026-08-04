# value -> store: pull a parsed field from a store...
namespaceOverride: [[ (ds "stores/network.yml").namespace ]]
commonAnnotations:
  net.example.com/cidr: "[[ (ds "stores/network.yml").ingressCIDR ]]"

# ...and embed a whole rendered manifest verbatim (raw include, indented for YAML).
extraDeploy:
  - |
[[ include "stores/netpol.yml" | strings.Indent 4 ]]
