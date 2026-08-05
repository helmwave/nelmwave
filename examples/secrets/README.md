# Example: encrypted values (sops)

Runnable as it stands — the age key is committed next to the files it decrypts:

```sh
SOPS_AGE_KEY_FILE=age.key ENV=stg nelmwave build
cat .nelmwave/values/api@app/*
```

> `age.key` is an example identity. It decrypts nothing but the two files here.
> Never reuse it.

Decryption happens **in-process**, through the sops library — the `sops` binary is
not required at deploy time. Keys come from the ambient environment exactly as
they do for the CLI: `SOPS_AGE_KEY_FILE`, `SOPS_AGE_KEY`, GnuPG, KMS. nelmwave
stores no key material of its own.

## Behaviour comes from the extension

Suffixes are peeled right to left:

| Source | Steps |
|---|---|
| `db.yml.sops` | decrypt |
| `tokens.yml.tpl.sops` | decrypt, **then** render as a gomplate template |
| `values.yml.tpl` | render only |
| `values.yml` | copy |

The order is not interchangeable: a template inside an encrypted file is
unreadable until decrypted. The artifact written under `.nelmwave/` drops those
suffixes (`00-db.yml`, not `00-db.yml.sops`), so no cleartext file is left looking
like ciphertext.

## Encrypting

A plain document encrypts normally, and keeps sops' per-value encryption — keys
stay readable in git, values do not:

```sh
sops --encrypt --age "$AGE_RECIPIENT" db.yml > db.yml.sops
```

A **template** must be encrypted as binary:

```sh
sops --encrypt --input-type binary --output-type binary \
  --age "$AGE_RECIPIENT" tokens.yml.tpl > tokens.yml.tpl.sops
```

This is not a preference. `[[ ... ]]` is a valid YAML flow sequence, so sops
encrypting a template *as YAML* reshapes the actions into nested lists and
destroys them. nelmwave therefore forces the binary format for `*.tpl.sops`, and
the file has to be written that way too.

## `.nelmwave/` holds cleartext

Decrypted values are written to disk so nelm can read them, and `build` says so:

```
WARN  decrypted secrets written in cleartext
      {"sources": 2, "dir": ".nelmwave",
       "hint": "treat this directory as sensitive: do not publish it as a build artifact"}
```

`.nelmwave/` is in `.gitignore`; the part worth checking is your CI, which will
happily archive it as a build artifact if told to.
