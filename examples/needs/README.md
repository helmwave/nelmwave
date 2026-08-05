# Example: dependencies between releases

Shows the three ways to say "apply that one first", and how a dependency outside
the selection behaves.

```sh
nelmwave build
cat .nelmwave/planfile.yml     # look at the resolved `needs:` of each release
```

## The three forms

| Form | Declares | Optional? |
|---|---|---|
| `needs.releases.<uniqname>` | one specific release | required, unless `optional: true` |
| `needs.matchLabels` | every release matching the labels | always optional |
| `needs.matchLabelsExpressions` | same, with `In`/`NotIn`/`Exists`/`DoesNotExist` | always optional |

A release depends on the **union** of all three. Label-matched needs are always
optional on purpose: a selector is a broad net, and a release it happens to catch
should not be able to fail your run.

`build` resolves the selectors, so the planfile lists concrete edges — that is the
file to check when a dependency does not look right.

## What the order buys you

Independent releases go **in parallel**; `--concurrency` bounds how many at once.
A failure stops that branch only: releases depending on the failed one are
skipped, unrelated branches finish.

```sh
nelmwave up --concurrency 2
```

`down` reverses every edge: dependents are removed before what they depend on.

## Dependencies outside the selection

This is where the three forms differ. With `-l app=api` only `api@app` is
selected, and its needs are not:

```sh
nelmwave up -l app=api
# postgres@data is required   -> error, nothing is applied
# redis@data is optional      -> warning, the edge is dropped

nelmwave up -l app=api --include-needs
# both are pulled back in and applied first
```

`--include-needs` travels the direction the command does — and that is **not** the
same direction for every command:

| Command | Pulls in | Here |
|---|---|---|
| `up`, `diff` | what the selection depends on | `up -l app=api --include-needs` also applies postgres and redis |
| `down` | what depends on the selection | `down -l app=postgres --include-needs` also removes api and worker |

The teardown case is the reason for the asymmetry: removing `postgres@data` while
`api@app` keeps running would leave the API talking to a database that is gone.

`diff` never refuses to plan: an unsatisfied dependency is an error for `up`, but
planning changes nothing, so it only widens the preview.

## Cycles

Cycles are rejected at build time, with the cycle printed — including cycles
formed through label selectors, which are easy to create by accident:

```yaml
a: { labels: { tier: x }, needs: { matchLabels: { tier: y } } }
b: { labels: { tier: y }, needs: { matchLabels: { tier: x } } }
```
