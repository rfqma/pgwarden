package config

import (
	"slices"
	"testing"
)

func TestChildEnv(t *testing.T) {
	environ := []string{
		"PATH=/usr/bin",
		"HOME=/home/pgwarden",
		"PGSSLMODE=verify-full",
		"PGSSLROOTCERT=/certs/ca.pem",
		"PGPASSWORD=from-env",
		"GPG_PASSPHRASE=gpg-secret",
		"S3_ACCESS_KEY=ak",
		"S3_SECRET_KEY=sk",
		"API_SECRET=api",
		"S3_BUCKET=b",
	}

	got := childEnv(environ, "from-config")

	for _, want := range []string{
		"PATH=/usr/bin",
		"HOME=/home/pgwarden",
		"PGSSLMODE=verify-full",
		"PGSSLROOTCERT=/certs/ca.pem",
		"S3_BUCKET=b",
		"PGPASSWORD=from-config",
	} {
		if !slices.Contains(got, want) {
			t.Errorf("env is missing %q", want)
		}
	}
	for _, leaked := range []string{
		"PGPASSWORD=from-env",
		"GPG_PASSPHRASE=gpg-secret",
		"S3_ACCESS_KEY=ak",
		"S3_SECRET_KEY=sk",
		"API_SECRET=api",
	} {
		if slices.Contains(got, leaked) {
			t.Errorf("env must not contain %q", leaked)
		}
	}
}

func TestChildEnvNoPassword(t *testing.T) {
	// Without a password, PGPASSWORD is left unset so libpq can fall back
	// to ~/.pgpass.
	for _, kv := range childEnv([]string{"PGPASSWORD=", "HOME=/h"}, "") {
		if kv == "PGPASSWORD=" {
			t.Fatal("empty PGPASSWORD passed to the child")
		}
	}
}
