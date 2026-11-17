package storage

import "testing"

func TestBackupKey(t *testing.T) {
	got, err := BackupKey("20260726_071210")
	if err != nil {
		t.Fatalf("BackupKey: %v", err)
	}
	if want := "backups/2026/07/20260726_071210.dump.gpg"; got != want {
		t.Fatalf("BackupKey = %q, want %q", got, want)
	}

	for _, id := range []string{"", "12", "2026", "20260726", "20261326_071210", "20260726_071210/../x", "../../etc"} {
		if _, err := BackupKey(id); err == nil {
			t.Errorf("BackupKey(%q) succeeded, want error", id)
		}
	}
}
