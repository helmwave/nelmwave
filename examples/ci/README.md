# Example: running in CI

The manifest is ordinary; what is worth showing is the command line. Three things
a pipeline needs that a laptop does not: a cluster without a kubeconfig, a gate on
drift, and a diff you can read in a log.

```sh
ENV=stg nelmwave build
cat .nelmwave/planfile.yml
```

## Connecting without a kubeconfig

In a pod the credentials are files, not a kubeconfig, so point at them directly:

```sh
nelmwave up \
  --kube-api-server "https://$KUBERNETES_SERVICE_HOST:$KUBERNETES_SERVICE_PORT" \
  --kube-token-path /var/run/secrets/kubernetes.io/serviceaccount/token \
  --kube-ca /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
```

Outside the cluster, with a token in a variable:

```sh
nelmwave up --kube-api-server https://k8s.example.com:6443 \
            --kube-token "$KUBE_TOKEN" --kube-ca ./ca.crt
```

Or hand over a whole kubeconfig without writing a file:

```sh
nelmwave up --kube-config-base64 "$KUBECONFIG_B64" --kube-context production
```

These are flags and not manifest fields on purpose: `build` copies the manifest
into `.nelmwave/planfile.yml`, so a token written there would travel with the
build artifacts. The same connection is used for nelmwave's own calls — the
namespace metadata it applies before nelm runs — so labels and workloads cannot
land in different clusters.

## Gating a job on drift

`diff --detailed-exitcode` exits 2 when changes are planned, following
`terraform plan` and `git diff --exit-code`, so nothing has to be parsed:

```sh
nelmwave diff --detailed-exitcode
case $? in
  0) echo "in sync" ;;
  2) echo "drift detected"; exit 1 ;;
  *) echo "diff failed"; exit 1 ;;
esac
```

Exit codes: `0` success, `1` failure, `2` changes planned (with that flag only).

## Making the diff readable in a log

```sh
# Default view, plus the annotations and managedFields normally stripped —
# for when a release reports changes you cannot see.
nelmwave diff --show-insignificant-diffs

# Quieter: skip the full manifests of resources created or deleted outright.
nelmwave diff --no-verbose-diffs
```

Do **not** add `--show-sensitive-diffs` in CI: it prints Secrets in the clear, into
the job log.

## Deploying in stages

```sh
nelmwave up -l 'tier=backend'                      # backend first
nelmwave up -l 'tier=batch' --include-needs        # then batch, dragging in what it needs
nelmwave up --concurrency 4                        # or everything, four at a time
```

## Machine-readable logs

`--log-format auto` already picks JSON when stderr is not a TTY, which is the case
in every CI runner. Force it if your runner allocates one:

```sh
nelmwave up --log-format json --log-level info
```

`--log-level` also drives nelm's own output, so `--log-level debug` gets the
engine's debug lines too, and `--log-level error` silences its progress tables —
worth it when several releases apply in parallel and their tables interleave.

## GitHub Actions

```yaml
- name: Build the plan
  env:
    ENV: production
  run: nelmwave build

- name: Fail on drift
  env:
    ENV: production
  run: nelmwave diff --detailed-exitcode
        --kube-api-server ${{ secrets.KUBE_API }}
        --kube-token ${{ secrets.KUBE_TOKEN }}
        --kube-ca-data "${{ secrets.KUBE_CA }}"
```

## GitLab CI

```yaml
deploy:
  variables:
    ENV: production
  script:
    - nelmwave build
    - nelmwave up --kube-api-server "$KUBE_API"
                  --kube-token "$KUBE_TOKEN"
                  --kube-ca-data "$KUBE_CA"
  artifacts:
    paths: []          # never archive .nelmwave/: it may hold decrypted values
```

Build once and apply that artifact if you prefer — `up`, `down` and `diff` never
re-render the manifest, so what a review job printed is what a deploy job applies.
