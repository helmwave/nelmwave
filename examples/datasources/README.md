# Example: cross-referencing artifacts via datasources

Shows how a release's `values` and `stores` can pull in **already-resolved**
artifacts of the same release through gomplate datasources.

## How it works

During `build`, for each release:

1. `stores` are resolved first, in order;
2. then `values`, in order.

Every resolved artifact is registered as a gomplate datasource named:

- `stores/<name>` for a store file,
- `values/<name>` for a values file,

where `<name>` is the entry's `name` (or an auto `NN-basename` when omitted).
A `.tpl` artifact can then reference an earlier one:

```gotemplate
[[ (ds "stores/network.yml").namespace ]]   # parsed — access fields
[[ include "stores/netpol.yml" ]]           # raw — embed the whole file
```

Ordering is **backward-only**: an item only sees artifacts resolved *before* it
in its own list; `values` additionally see all `stores`. It is scoped to a
single release. A reference to a skipped `optional` artifact renders empty.

## Layout

```
nelmwave.yml.tpl
shared/network.yml           # plain shared data (a store source)
templates/netpol.yml.tpl     # a store rendered FROM stores/network.yml
values/app.yml.tpl           # a value rendered FROM the stores
```

## Try it

```sh
nelmwave build
cat .nelmwave/store/web@frontend/netpol.yml     # store <- store
cat .nelmwave/values/web@frontend/00-app.yml    # value <- store
```

`netpol.yml` gets its `namespace`/`cidr` from `stores/network.yml`; `app.yml`
reads the same fields and embeds the fully rendered `netpol.yml` verbatim.
