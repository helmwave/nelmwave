package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Release state lives either in the cluster or in a database. nelm picks the
// implementation by a bare driver name; nelmwave takes a URL instead, so one
// field carries both the choice and its parameters — the same trick
// Repositories use to tell a helm repo from an OCI registry.
//
//	kubernetes://secrets            # the default
//	kubernetes://configmaps
//	psql://nelm@db.internal/nelm    # PostgreSQL, password from the environment
const (
	// DriverSchemeKubernetes stores state in the release namespace.
	DriverSchemeKubernetes = "kubernetes"
	// DriverSchemePsql and its aliases store state in PostgreSQL.
	DriverSchemePsql       = "psql"
	DriverSchemePostgres   = "postgres"
	DriverSchemePostgresql = "postgresql"
)

// StorageDriver is a parsed driverURL: what to tell nelm, and — for SQL — how
// to connect.
type StorageDriver struct {
	// Driver is nelm's ReleaseStorageDriver ("secrets", "configmaps", "sql").
	// Empty means the manifest said nothing and nelm's default applies.
	Driver string
	// SQLConnection is the libpq connection string, set only for sql.
	SQLConnection string
	// HasPassword reports whether the URL embedded a password. Callers warn
	// about it: a password in the manifest is written to the planfile as-is.
	HasPassword bool
}

// ParseDriverURL turns a driverURL into a StorageDriver. An empty string is
// valid and selects nelm's default (secrets).
//
// "memory" is deliberately unsupported. nelm has such a driver, but state that
// dies with the process cannot work here: the next up would find no history,
// treat the release as new, and try to adopt the resources it installed itself.
func ParseDriverURL(raw string) (StorageDriver, error) {
	if raw == "" {
		return StorageDriver{}, nil
	}

	u, err := url.Parse(raw)
	if err != nil {
		return StorageDriver{}, fmt.Errorf("invalid driverURL %q: %w", raw, err)
	}

	switch u.Scheme {
	case DriverSchemeKubernetes:
		// kubernetes://secrets — the "host" names the object kind.
		switch kind := strings.ToLower(u.Host); kind {
		case "secret", "secrets":
			return StorageDriver{Driver: "secrets"}, nil
		case "configmap", "configmaps":
			return StorageDriver{Driver: "configmaps"}, nil
		default:
			return StorageDriver{}, fmt.Errorf(
				"invalid driverURL %q: unknown kubernetes object %q (want secrets or configmaps)", raw, kind)
		}
	case DriverSchemePsql, DriverSchemePostgres, DriverSchemePostgresql:
		// nelm hands the string to lib/pq, which only knows postgres(ql)://.
		conn := *u
		conn.Scheme = DriverSchemePostgres
		_, hasPassword := u.User.Password()
		return StorageDriver{
			Driver:        "sql",
			SQLConnection: conn.String(),
			HasPassword:   hasPassword,
		}, nil
	default:
		return StorageDriver{}, fmt.Errorf(
			"invalid driverURL %q: unknown scheme %q (want kubernetes://, psql://)", raw, u.Scheme)
	}
}
