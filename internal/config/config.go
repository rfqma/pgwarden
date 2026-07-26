package config

import (
	"fmt"
	"os"
	"strconv"
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
			Port:   getEnv("API_PORT", "8080"),
			Secret: getEnv("API_SECRET", ""),
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

func (c *PostgreSQLConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s",
		c.Host, c.Port, c.User, c.Password, c.Database,
	)
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

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	i, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return i
}

