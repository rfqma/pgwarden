package gpg

import (
	"bytes"
	"os"
	"os/exec"
	"testing"
)

// isolatedGPGHome points gpg at a throwaway home so the tests never touch
// the user's keyring. os.MkdirTemp keeps the path short enough for the
// gpg-agent socket on macOS, which t.TempDir() can exceed.
func isolatedGPGHome(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not installed")
	}
	dir, err := os.MkdirTemp("", "pgw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		exec.Command("gpgconf", "--homedir", dir, "--kill", "gpg-agent").Run()
		os.RemoveAll(dir)
	})
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", dir)
}

func TestRoundTrip(t *testing.T) {
	isolatedGPGHome(t)
	plain := []byte("pg_dump custom-format bytes \x00\x01\x02")
	const pass = "correct horse battery staple"

	enc, err := Encrypt(plain, pass)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if bytes.Contains(enc, plain) {
		t.Fatal("ciphertext contains the plaintext")
	}

	dec, err := Decrypt(enc, pass)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("Decrypt = %q, want %q", dec, plain)
	}
}

func TestDecryptWrongPassphrase(t *testing.T) {
	isolatedGPGHome(t)
	enc, err := Encrypt([]byte("secret data"), "right")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := Decrypt(enc, "wrong"); err == nil {
		t.Fatal("Decrypt with the wrong passphrase succeeded")
	}
}

// Backups made before the passphrase moved to a pipe were encrypted with
// `--passphrase <arg>`; they must still decrypt.
func TestDecryptsLegacyArgEncryptedBackup(t *testing.T) {
	isolatedGPGHome(t)
	const pass = "legacy-pass"
	plain := []byte("old backup")

	cmd := exec.Command("gpg", "--batch", "--yes", "--pinentry-mode", "loopback",
		"--symmetric", "--cipher-algo", "AES256", "--passphrase", pass, "--output", "-")
	cmd.Stdin = bytes.NewReader(plain)
	enc, err := cmd.Output()
	if err != nil {
		t.Fatalf("legacy encrypt: %v", err)
	}

	dec, err := Decrypt(enc, pass)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if !bytes.Equal(dec, plain) {
		t.Fatalf("Decrypt = %q, want %q", dec, plain)
	}
}
