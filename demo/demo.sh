#!/usr/bin/env bash
# Scripted walkthrough of nelmwave, meant to be captured with asciinema:
#
#   make e2e-up && make demo && make e2e-down
#
# It runs the whole loop — build, diff, up, diff again, selective up, down —
# against the throwaway k3s cluster the e2e fixture starts. Everything it
# installs comes from demo/project/chart, so nothing but registry.k8s.io/pause
# is pulled.
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PATH="$ROOT:$PATH"   # the freshly built ./nelmwave wins over any installed one
export PATH

# The cast must never reach a real cluster, so the kubeconfig is the fixture's
# and the API server behind it has to be a loopback address. Anything else is a
# hard stop rather than a "probably fine".
export KUBECONFIG="${DEMO_KUBECONFIG:-$ROOT/test/e2e/.kube/kubeconfig.yaml}"
if [[ ! -f $KUBECONFIG ]]; then
  printf 'no kubeconfig at %s — run `make e2e-up` first\n' "$KUBECONFIG" >&2
  exit 1
fi
server=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}' 2>/dev/null)
if [[ ! $server =~ ^https://(127\.0\.0\.1|localhost|\[::1\]): ]]; then
  printf 'refusing to record against %s — the demo only runs on a local cluster\n' "$server" >&2
  exit 1
fi

# A rerun must start from an empty cluster, or the cast would open on "no
# changes" instead of on the creates it is meant to show.
(
  cd "$ROOT/demo/project" || exit 0
  nelmwave build --log-level error >/dev/null 2>&1 || exit 0
  nelmwave down --log-level error >/dev/null 2>&1
  rm -rf .nelmwave
)
kubectl delete ns demo-app demo-data --ignore-not-found --wait >/dev/null 2>&1

DIM=$'\033[38;5;244m'
CYAN=$'\033[38;5;80m'
GREEN=$'\033[38;5;114m'
BOLD=$'\033[1m'
OFF=$'\033[0m'

TYPE_DELAY=0.022   # per-character typing delay
BEAT=0.7           # pause after a command finishes
READ=1.8           # pause on output worth reading

pause() { sleep "$1"; }

# say prints a commentary line, as a shell comment above the command.
say() {
  printf '%s# %s%s\n' "$DIM" "$1" "$OFF"
  pause 0.4
}

# run types a command out character by character, then runs it.
run() {
  printf '%s❯%s ' "$CYAN" "$OFF"
  local i
  for ((i = 0; i < ${#1}; i++)); do
    printf '%s' "${1:i:1}"
    sleep "$TYPE_DELAY"
  done
  printf '\n'
  pause 0.3
  eval "$1"
  pause "$BEAT"
}

clear
printf '%s%snelmwave%s — many releases, one declarative manifest, applied through nelm.\n' \
  "$BOLD" "$GREEN" "$OFF"
printf '%sRecorded against the throwaway k3s cluster from test/e2e.%s\n\n' "$DIM" "$OFF"
pause 1.2

cd "$ROOT" || exit 1
run "nelmwave --version"

# ---------------------------------------------------------------- the manifest
say "Three releases, two namespaces, one local chart. Dependencies declared two ways."
cd "$ROOT/demo/project" || exit 1
run "bat --paging=never --color=always --style=numbers --line-range=15:41 nelmwave.yml.tpl"
pause "$READ"

# ---------------------------------------------------------------------- build
say "build renders the manifest, resolves values and writes the plan. No cluster yet."
run "nelmwave build"
run "tree .nelmwave"
pause "$READ"

# ----------------------------------------------------------------------- diff
say "diff asks nelm what it would do — nothing exists, so everything is a create."
run "nelmwave diff -l 'app=postgres'"
pause "$READ"

# ------------------------------------------------------------------------- up
say "up applies the plan in dependency order: postgres first, then api and cache in parallel."
run "nelmwave up"
pause 1.0
run "kubectl get deploy,cm -A -l app.kubernetes.io/managed-by=Helm"
pause "$READ"

# ------------------------------------------------------------------ no drift
say "Nothing changed since the apply, so --detailed-exitcode reports 0."
run "nelmwave diff --detailed-exitcode -l 'app=postgres'; echo \"exit=\$?\""
pause 1.2

# --------------------------------------------------------------------- drift
say "Change an input, rebuild the plan: MSG travels env -> gomplate -> plan -> chart."
run "MSG=v2 nelmwave build"
say "Now there is drift, and CI can gate on exit code 2."
run "nelmwave diff --detailed-exitcode -l 'app=api'; echo \"exit=\$?\""
pause "$READ"

# ----------------------------------------------------------- selective apply
say "Apply just the api tier; --include-needs pulls its dependency back in."
run "nelmwave up -l 'app=api' --include-needs"
run "kubectl get cm api -n demo-app -o jsonpath='{.data.message}'; echo"
pause "$READ"

# ------------------------------------------------------------------------ down
say "down uninstalls in reverse dependency order: api and cache first, postgres last."
run "nelmwave down"
run "kubectl get deploy,cm -A -l app.kubernetes.io/managed-by=Helm"
pause 1.0

printf '\n%s%s→ examples/ has a runnable project per feature area.%s\n\n' "$BOLD" "$GREEN" "$OFF"
pause 2.0
