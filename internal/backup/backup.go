package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/maruarchive/pgwarden/internal/config"
	"github.com/maruarchive/pgwarden/internal/gpg"
	"github.com/maruarchive/pgwarden/internal/storage"
)

type Result struct {
	ID        string
	Size      int64
	Duration  time.Duration
	Checksum  string
	CreatedAt time.Time
}

var (
	// mu serializes backups in this process, so two requests cannot share
	// an ID (IDs have one-second resolution) or hold two dumps in memory.
	mu     sync.Mutex
	lastID string
)

// nextID returns the time and ID for a new backup. If the clock is still in
// the second of the previous backup, it waits for the next second, so IDs
// handed out by one process never repeat. Callers must hold mu.
func nextID(now func() time.Time, sleep func(time.Duration)) (time.Time, string) {
	t := now()
	id := t.Format(storage.BackupIDLayout)
	if id == lastID {
		sleep(t.Truncate(time.Second).Add(time.Second).Sub(t))
		t = now()
		id = t.Format(storage.BackupIDLayout)
	}
	lastID = id
	return t, id
}

func Run(ctx context.Context, cfg *config.Config, store *storage.S3Storage) (*Result, error) {
	mu.Lock()
	defer mu.Unlock()

	start, backupID := nextID(time.Now, time.Sleep)

	dumpKey, err := storage.BackupKey(backupID)
	if err != nil {
		return nil, err
	}

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
	cmd.Env = cfg.Postgres.Env()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pg_dump failed: %s: %w", stderr.String(), err)
	}

	dumpData := stdout.Bytes()

	encrypted, err := gpg.Encrypt(dumpData, cfg.Backup.GPGPassphrase)
	if err != nil {
		return nil, fmt.Errorf("encryption failed: %w", err)
	}

	dumpSize := int64(len(encrypted))

	checksum := sha256.Sum256(dumpData)
	checksumStr := hex.EncodeToString(checksum[:])

	// Another pgwarden process (the CLI next to the API, or a second
	// replica) may have made a backup in the same second. Never overwrite it.
	exists, err := store.Exists(ctx, dumpKey)
	if err != nil {
		return nil, fmt.Errorf("S3 check failed: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("backup %s already exists (another pgwarden made a backup in the same second); not overwriting it, try again", backupID)
	}

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
