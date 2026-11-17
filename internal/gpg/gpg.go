// Package gpg encrypts and decrypts backups with GPG symmetric AES-256.
//
// The passphrase is handed to gpg over an inherited pipe (--passphrase-fd)
// rather than as a command-line argument, so it never shows up in `ps` or
// /proc/<pid>/cmdline.
package gpg

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// passphraseFD is the file descriptor gpg reads the passphrase from. The
// first entry of exec.Cmd.ExtraFiles becomes fd 3 in the child.
const passphraseFD = "3"

// Encrypt encrypts data with a symmetric AES-256 key derived from passphrase.
func Encrypt(data []byte, passphrase string) ([]byte, error) {
	return run(data, passphrase, "--symmetric", "--cipher-algo", "AES256")
}

// Decrypt decrypts data produced by Encrypt.
func Decrypt(data []byte, passphrase string) ([]byte, error) {
	return run(data, passphrase, "--decrypt")
}

// homeOnce creates gpg's home directory before the first gpg runs. Two gpg
// processes started together on a fresh home both try to create it, and the
// loser fails with "can't create directory ... File exists".
var homeOnce sync.Once

func ensureHome() {
	dir := os.Getenv("GNUPGHOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return // let gpg report it
		}
		dir = filepath.Join(home, ".gnupg")
	}
	os.MkdirAll(dir, 0o700)
}

func run(data []byte, passphrase string, op ...string) ([]byte, error) {
	gpgPath, err := exec.LookPath("gpg")
	if err != nil {
		return nil, fmt.Errorf("gpg not found: %w", err)
	}
	homeOnce.Do(ensureHome)

	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("passphrase pipe: %w", err)
	}

	args := append([]string{
		"--batch",
		"--yes",
		"--pinentry-mode", "loopback",
		"--passphrase-fd", passphraseFD,
		"--output", "-",
	}, op...)

	cmd := exec.Command(gpgPath, args...)
	cmd.Stdin = bytes.NewReader(data)
	cmd.ExtraFiles = []*os.File{pr}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		pr.Close()
		pw.Close()
		return nil, err
	}
	// The child holds its own copy of the read end now.
	pr.Close()

	// Write from a goroutine so a passphrase larger than the pipe buffer
	// cannot block us before gpg starts reading.
	go func() {
		pw.WriteString(passphrase)
		pw.Close()
	}()

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}
