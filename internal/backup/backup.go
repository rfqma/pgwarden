package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"time"

	"github.com/maruarchive/pgwarden/internal/config"
	"github.com/maruarchive/pgwarden/internal/storage"
)

type Result struct {
	ID        string
	Size      int64
	Duration  time.Duration
	Checksum  string
	CreatedAt time.Time
}

func Run(ctx context.Context, cfg *config.Config, store *storage.S3Storage) (*Result, error) {
	start := time.Now()
	backupID := start.Format("20060102_150405")

	dumpKey := fmt.Sprintf("backups/%s/%s.dump.gpg",
		start.Format("2006/01"), backupID)

	args := []string{
		"-Fc",
		"-h", cfg.Postgres.Host,
		"-p", cfg.Postgres.Port,
		"-U", cfg.Postgres.User,
		"-d", cfg.Postgres.Database,
		"--no-owner",
		"--no-privileges",
	}

	pgDumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		return nil, fmt.Errorf("pg_dump not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, pgDumpPath, args...)
	cmd.Env = append(cmd.Env,
		fmt.Sprintf("PGPASSWORD=%s", cfg.Postgres.Password),
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pg_dump failed: %s: %w", stderr.String(), err)
	}

	dumpData := stdout.Bytes()

	encrypted, err := encrypt(dumpData, cfg.Backup.GPGPassphrase)
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}

	dumpSize := int64(len(encrypted))

	checksum := sha256.Sum256(dumpData)
	checksumStr := hex.EncodeToString(checksum[:])

	reader := bytes.NewReader(encrypted)
	if err := store.Upload(ctx, dumpKey, reader, dumpSize); err != nil {
		return nil, fmt.Errorf("S3 upload failed: %w", err)
	}

	duration := time.Since(start)

	result := &Result{
		ID:        backupID,
		Size:      dumpSize,
		Duration:  duration,
		Checksum:  checksumStr,
		CreatedAt: start,
	}

	fmt.Printf("[pgwarden] Backup completed: %s (%d bytes, %s)\n",
		backupID, dumpSize, duration.Round(time.Millisecond))

	return result, nil
}

func encrypt(data []byte, passphrase string) ([]byte, error) {
	cmd := exec.Command("gpg",
		"--batch",
		"--yes",
		"--symmetric",
		"--cipher-algo", "AES256",
		"--passphrase", passphrase,
		"--output", "-",
	)

	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

func decrypt(data []byte, passphrase string) ([]byte, error) {
	cmd := exec.Command("gpg",
		"--batch",
		"--yes",
		"--decrypt",
		"--passphrase", passphrase,
	)

	cmd.Stdin = bytes.NewReader(data)
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}
