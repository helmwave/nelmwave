# store -> store: this store is rendered from the earlier "network.yml" store.
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-ingress
  namespace: [[ (ds "stores/network.yml").namespace ]]
spec:
  podSelector: {}
  policyTypes: [Ingress]
  ingress:
    - from:
        - ipBlock:
            cidr: [[ (ds "stores/network.yml").ingressCIDR ]]
