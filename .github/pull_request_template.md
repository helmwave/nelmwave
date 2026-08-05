<!--
Trunk-based development: small PR, short-lived branch, merged into main the same
day if at all possible. Anything bigger than that wants to be split.
-->

## What changed

<!-- One paragraph. Why, not just what. -->

## Checklist

- [ ] `make test` and `make lint` pass locally
- [ ] `changie new` fragment committed under `.changes/unreleased/`
      (or the `skip-changelog` label applied — CI-only or cosmetic changes)
- [ ] A new manifest field also touched: `config/` → `validate.go` → `plan/` →
      `release/` → tests → README table → `examples/` (see CLAUDE.md), and
      `make examples` passes
- [ ] No secrets in a manifest or in `examples/` — `build` copies the manifest
      into `planfile.yml` verbatim
