# Where release state lives: driverURL, one field carrying both the choice of
# storage and its parameters.
project: storage

Release:
  # Set it once for the whole manifest. A manifest whose releases keep state in
  # different places is a good way to lose one.
  driverURL: kubernetes://secrets      # the default, spelled out

releases:
  api@app:
    labels: { app: api }
    chart: { name: ../charts/stub }

  # Helm 2 kept state in ConfigMaps; this is here for compatibility with what
  # came before. Mind the permissions: release state includes rendered values,
  # and a ConfigMap is readable by anyone who can `get configmaps`.
  legacy@app:
    labels: { app: legacy }
    chart: { name: ../charts/stub }
    driverURL: kubernetes://configmaps

  # PostgreSQL, for releases that outgrow the ~1 MB a Secret can hold, or when
  # history should outlive the namespace.
  #
  # No password in the URL: `build` copies the manifest into the planfile, so it
  # would sit in cleartext on disk and in CI artifacts. libpq reads PGPASSWORD,
  # and `build` warns if it finds a password here anyway.
  huge@app:
    labels: { app: huge }
    chart: { name: ../charts/stub }
    driverURL: psql://nelm@db.internal:5432/nelm?sslmode=require
