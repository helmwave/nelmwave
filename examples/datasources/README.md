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
[[ (ds "values/base.yml").replicaCount ]]   # parsed — access fields
[[ include "stores/netpol.yml" ]]           # raw — embed the whole file
```

Ordering is **backward-only**: an item only sees artifacts resolved *before* it
in its own list; `values` additionally see all `stores`. It is scoped to a
single release. A reference to a skipped `optional` artifact renders empty.

## values -> values

The most common case: declare a knob once in an early values file, then derive
from it in a later one instead of repeating the number.

```yaml
values:
  - { src: values/base.yml, name: base.yml }      # plain data
  - { src: values/app.yml.tpl, name: app.yml }    # reads values/base.yml
```

```yaml
# values/base.yml
replicaCount: 2
image:
  tag: 1.27.4
```

```gotemplate
[[- $base := ds "values/base.yml" -]]
autoscaling:
  minReplicas: [[ $base.replicaCount ]]
  maxReplicas: [[ math.Mul $base.replicaCount 4 ]]
podAnnotations:
  app.example.com/image-tag: "[[ $base.image.tag ]]"
```

Give both entries an explicit `name` — that name *is* the datasource key. Without
it the artifact gets an auto `NN-basename` (`00-base`, `01-app`), which shifts
whenever the list is reordered.

Both files are still passed to the chart, in order, so `base.yml` keeps working
as ordinary values while `app.yml` only adds the derived fields.

## Layout

```
nelmwave.yml.tpl
shared/network.yml           # plain shared data (a store source)
templates/netpol.yml.tpl     # a store rendered FROM stores/network.yml
values/base.yml              # plain values, the source of truth for sizing
values/app.yml.tpl           # a value rendered FROM values/base.yml + the stores
```

## Try it

```sh
nelmwave build
cat .nelmwave/store/web@frontend/netpol.yml   # store <- store
cat .nelmwave/values/web@frontend/app.yml     # value <- value, value <- store
```

`netpol.yml` gets its `namespace`/`cidr` from `stores/network.yml`; `app.yml`
derives its replica/image fields from `values/base.yml`, reads the same network
fields, and embeds the fully rendered `netpol.yml` verbatim.
