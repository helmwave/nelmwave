# demo

`nelmwave.cast` is a ~1.5 minute [asciicast](https://docs.asciinema.org/manual/asciicast/v3/)
of the whole loop against a real cluster:

`build` → `diff` (everything is a create) → `up` (postgres first, then api and
cache in parallel) → `diff --detailed-exitcode` (exit 0, no drift) → rebuild with
a changed input → `diff` again (exit 2, drift) → `up -l app=api --include-needs`
→ `down` (reverse dependency order).

Play it locally:

```sh
asciinema play demo/nelmwave.cast
```

Re-record it after a UX change (needs `asciinema`, `bat`, `tree` and `kubectl`):

```sh
make e2e-up        # throwaway k3s from test/e2e/docker-compose.yml
make demo
make e2e-down
```

## What it records

`project/` is the manifest: three releases across two namespaces, installing
`project/chart` (a ConfigMap and a `pause` Deployment), so nothing is downloaded
but `registry.k8s.io/pause`. `MSG` is the changed input — it travels shell env →
gomplate → planfile → chart → ConfigMap, which is what the drift step shows.

`demo.sh` is the script. Two safeguards, because a cast that deploys is a cast
that can deploy to the wrong place:

- it uses the fixture's kubeconfig (`test/e2e/.kube/kubeconfig.yaml`, override
  with `DEMO_KUBECONFIG`) and **refuses to run** unless the API server behind it
  is a loopback address;
- it uninstalls anything left over from a previous run first, so the recording
  opens on creates rather than on no-ops.
