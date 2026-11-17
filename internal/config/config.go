package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Postgres PostgreSQLConfig
	S3       S3Config
	Backup   BackupConfig
	API      APIConfig
}

type PostgreSQLConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

type S3Config struct {
	Endpoint  string
	Bucket    string
	Prefix    string
	Region    string
	AccessKey string
	SecretKey string
	UseSSL    bool
}

type BackupConfig struct {
	GPGPassphrase string
}

type APIConfig struct {
	Port   string
	Secret string
	// Insecure lets the API server run without API_SECRET. Development only:
	// without a secret anyone who reaches the port can restore or delete.
	Insecure bool
}

func Load() (*Config, error) {
	cfg := &Config{
		Postgres: PostgreSQLConfig{
			Host:     getEnv("PGHOST", "localhost"),
			Port:     getEnv("PGPORT", "5432"),
			User:     getEnv("PGUSER", "postgres"),
			Password: getEnv("PGPASSWORD", ""),
			Database: getEnv("PGDATABASE", "postgres"),
		},
		S3: S3Config{
			Endpoint:  getEnv("S3_ENDPOINT", "s3.amazonaws.com"),
			Bucket:    getEnv("S3_BUCKET", ""),
			Prefix:    getEnv("S3_PREFIX", "pgwarden"),
			Region:    getEnv("S3_REGION", ""),
			AccessKey: getEnv("S3_ACCESS_KEY", ""),
			SecretKey: getEnv("S3_SECRET_KEY", ""),
			UseSSL:    getEnvBool("S3_USE_SSL", true),
		},
		Backup: BackupConfig{
			GPGPassphrase: getEnv("GPG_PASSPHRASE", ""),
		},
		API: APIConfig{
			Port:     getEnv("API_PORT", "8080"),
			Secret:   getEnv("API_SECRET", ""),
			Insecure: getEnvBool("API_INSECURE", false),
		},
	}

	if cfg.S3.Bucket == "" {
		return nil, fmt.Errorf("S3_BUCKET is required")
	}

	if cfg.Backup.GPGPassphrase == "" {
		return nil, fmt.Errorf("GPG_PASSPHRASE is required — all backups are encrypted")
	}

	return cfg, nil
}

// secretEnv are pgwarden's own secrets, which pg_dump and pg_restore have no
// use for and so do not inherit.
var secretEnv = map[string]bool{
	"GPG_PASSPHRASE": true,
	"S3_ACCESS_KEY":  true,
	"S3_SECRET_KEY":  true,
	"API_SECRET":     true,
}

// Env returns the environment for pg_dump and pg_restore. It keeps
// pgwarden's own environment, so libpq settings such as PGSSLMODE,
// PGSSLROOTCERT, PGOPTIONS and PGCONNECT_TIMEOUT (and HOME, for ~/.pgpass)
// reach them, drops pgwarden's secrets, and sets PGPASSWORD from the config.
func (c *PostgreSQLConfig) Env() []string {
	return childEnv(os.Environ(), c.Password)
}

func childEnv(environ []string, password string) []string {
	env := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if secretEnv[key] || key == "PGPASSWORD" {
			continue
		}
		env = append(env, kv)
	}
	if password != "" {
		env = append(env, "PGPASSWORD="+password)
	}
	return env
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
