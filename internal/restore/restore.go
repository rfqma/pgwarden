package restore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"time"

	"github.com/maruarchive/pgwarden/internal/config"
	"github.com/maruarchive/pgwarden/internal/gpg"
	"github.com/maruarchive/pgwarden/internal/storage"
)

func Run(ctx context.Context, cfg *config.Config, store *storage.S3Storage, backupID string) error {
	start := time.Now()

	dumpKey, err := storage.BackupKey(backupID)
	if err != nil {
		return err
	}

	reader, err := store.Download(ctx, dumpKey)
	if err != nil {
		return fmt.Errorf("S3 download failed: %w", err)
	}
	defer reader.Close()

	dumpData, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("failed to read backup: %w", err)
	}

	decrypted, err := gpg.Decrypt(dumpData, cfg.Backup.GPGPassphrase)
	if err != nil {
		return fmt.Errorf("decryption failed: %w", err)
	}

	args := []string{
		"-h", cfg.Postgres.Host,
		"-p", cfg.Postgres.Port,
		"-U", cfg.Postgres.User,
		"-d", cfg.Postgres.Database,
		"--no-owner",
		"--no-privileges",
		"--clean",
		"--if-exists",
		"--single-transaction",
	}

	pgRestorePath, err := exec.LookPath("pg_restore")
	if err != nil {
		return fmt.Errorf("pg_restore not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, pgRestorePath, args...)
	cmd.Env = cfg.Postgres.Env()
	cmd.Stdin = bytes.NewReader(decrypted)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pg_restore failed: %s: %w", stderr.String(), err)
	}

	fmt.Printf("[pgwarden] Restore completed: %s (%s)\n",
		backupID, time.Since(start).Round(time.Millisecond))

	return nil
}
