# Contributing

## Trunk-based development

`main` is the trunk and it is always releasable. There are no long-lived
branches: no `develop`, no `release-1.x`, no maintenance forks.

- Branch off `main`, keep the branch alive for hours or a day — not a week.
- One PR does one thing. If it needs a section heading to explain, split it.
- Merge into `main` with a squash merge, so one PR is one trunk commit.
- Commit messages are `feat: <short summary>` (see CLAUDE.md); the squash
  commit title is what ends up on the trunk.
- Never push directly to `main`. Everything goes through a PR, including
  dependency bumps and the release PR itself.
- Anything not ready to be visible ships behind a flag or an unwired code path,
  not behind an unmerged branch.

## Before you open a PR

```sh
make test      # unit tests, no cluster
make lint      # golangci-lint, config pinned in .golangci.yml
make examples  # builds every examples/*/nelmwave.yml.tpl — catches schema drift
make e2e       # real k3s in docker-compose; CI runs this too
```

## Changelog fragments

`CHANGELOG.md` is generated — do not edit it. Every PR that changes behaviour
adds a [changie](https://changie.dev) fragment instead:

```sh
changie new    # asks for a kind, a body, and optionally an issue number
git add .changes/unreleased/
```

This is what makes a busy trunk work: two PRs adding a fragment never conflict,
while two PRs editing `CHANGELOG.md` always do.

CI enforces it (`changelog fragment` job). A change that genuinely needs no
entry — CI config, a typo, a refactor with no visible effect — gets the
`skip-changelog` label instead.

Kinds and the version bump each implies (`.changie.yaml`):

| Kind | Bump |
|---|---|
| Removed | major |
| Added, Changed | minor |
| Deprecated, Fixed, Security, Dependencies | patch |

v1.0.0 shipped, so semver applies in full: one `Removed` fragment is enough to
make the next release a major one. A breaking change that removes nothing has no
kind of its own — file it under `Removed`, or run the release-pr workflow with an
explicit `major` bump.

Dependabot cannot run `changie new`, so its PRs skip this check entirely; the
`Dependencies` kind is for a bump a human decides is worth announcing.

## Releasing

Two steps, both in GitHub Actions — no one tags by hand.

1. Run the **release-pr** workflow (Actions tab, or `gh workflow run
   release-pr.yml`). Pick a bump (`auto` derives it from the fragment kinds) and
   optionally a prerelease suffix like `rc.1`. It batches the fragments,
   regenerates `CHANGELOG.md` and opens a `release/vX.Y.Z` PR.
2. Review that PR — it is the last point where the release notes can be fixed —
   and merge it. The **release** workflow then tags the merge commit and runs
   goreleaser: archives and checksums on the GitHub release, three multi-arch
   images on `ghcr.io/helmwave/nelmwave`, and the Homebrew formula in
   `helmwave/homebrew-tap`.

Image flavours, one Dockerfile target each:

| Tags | Target | Contents |
|---|---|---|
| `X.Y.Z`, `X.Y`, `latest` | `goreleaser` | alpine, ca-certificates, non-root (65534) |
| `X.Y.Z-scratch`, `latest-scratch` | `scratch-goreleaser` | the binary, ca-certificates and a 1777 `/tmp` — nothing else |
| `X.Y.Z-debug`, `latest-debug` | `debug-goreleaser` | the default plus bash, jq, kubectl; runs as root |

`docker build --target release .` compiles inside the image instead, for anyone
building without goreleaser. Pass `--build-arg VERSION=$(git describe --tags)`
(plus `COMMIT` and `DATE`) to stamp `internal/version`; the defaults match its
own `dev`/`none`/`unknown` fallbacks.

A prerelease never moves `latest`, `X.Y` or the `latest-*` aliases — an rc
publishes only its exact version tags.

A failed publish can be re-run: **release** is idempotent — it skips the tag if
it already exists and goreleaser replaces the release assets.

## What CI checks

| Job | What it protects |
|---|---|
| `lint` | golangci-lint and formatting |
| `test` | unit tests with `-race` on Linux and macOS |
| `examples` | the documented manifests still build |
| `e2e` | a real apply against k3s |
| `security` | `govulncheck` (new reachable CVEs only, see `.github/govulncheck-allow.txt`) and `gosec` |
| `changelog fragment` | a changie entry exists |
| `release config` | `.goreleaser.yaml` still validates and builds |
