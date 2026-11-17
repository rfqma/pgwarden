package storage

import (
	"fmt"
	"time"
)

// BackupIDLayout is the time layout of a backup ID, e.g. 20260726_071210.
const BackupIDLayout = "20060102_150405"

// BackupKey returns the object key (before S3_PREFIX) of the backup with
// the given ID: backups/YYYY/MM/<id>.dump.gpg. It rejects anything that is
// not a valid backup ID, so callers never slice a short or malformed ID.
func BackupKey(id string) (string, error) {
	t, err := time.Parse(BackupIDLayout, id)
	if err != nil {
		return "", fmt.Errorf("invalid backup ID %q: want YYYYMMDD_HHMMSS, e.g. 20260726_071210", id)
	}
	return fmt.Sprintf("backups/%s/%s.dump.gpg", t.Format("2006/01"), id), nil
}
