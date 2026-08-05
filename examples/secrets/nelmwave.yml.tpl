# Encrypted values: *.sops files are decrypted in-process during build, so the
# repository holds ciphertext and .nelmwave/ holds the cleartext it resolved.
project: secrets

releases:
  api@app:
    labels: { app: api }
    chart: { name: ../charts/stub }
    values:
      # Behaviour comes from the extension, right to left:
      #
      #   db.yml.sops         -> decrypt
      #   tokens.yml.tpl.sops -> decrypt, then render as a gomplate template
      #
      # The artifact on disk is named without those suffixes, so no cleartext
      # file ends up called "*.sops".
      - values/db.yml.sops
      - values/tokens.yml.tpl.sops
