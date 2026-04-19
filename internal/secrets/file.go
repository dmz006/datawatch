// File-backed secret provider — reads/writes 0600 files under a
// caller-supplied base directory. Default for single-host
// deployments + dev. Always available; no external dependencies.

package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileProvider stores each secret as one file under BaseDir, keyed
// by sanitised filename. 0600 perms enforced on write; parent dir
// created at 0700 if missing.
type FileProvider struct {
	BaseDir string
}

// NewFileProvider builds a FileProvider rooted at baseDir. Empty
// baseDir defaults to ~/.datawatch/secrets/ (operator can override
// per-Cluster-Profile).
func NewFileProvider(baseDir string) *FileProvider {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".datawatch", "secrets")
	}
	return &FileProvider{BaseDir: baseDir}
}

// Kind implements Provider.
func (*FileProvider) Kind() string { return "file" }

// Get reads the file at BaseDir/key. Returns ErrNotFound when the
// file doesn't exist; surfaces other errors verbatim.
func (p *FileProvider) Get(key string) (string, error) {
	path, err := p.path(key)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w (key %q at %s)", ErrNotFound, key, path)
	}
	if err != nil {
		return "", err
	}
	// Trim trailing newline — operators editing in $EDITOR add one.
	return strings.TrimRight(string(data), "\n"), nil
}

// Put writes value to BaseDir/key with 0600 perms (parent at 0700).
// Atomic via tmp-then-rename so a crash mid-write doesn't leave a
// half-written secret.
func (p *FileProvider) Put(key, value string) error {
	path, err := p.path(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("secrets file: mkdir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(value), 0600); err != nil {
		return fmt.Errorf("secrets file: write: %w", err)
	}
	return os.Rename(tmp, path)
}

// path resolves key to a filesystem path under BaseDir, refusing
// path-traversal attempts (../ + absolute paths). Keys may contain
// slashes for namespacing (e.g. "git/github/dmz006") which are
// treated as subdirectories.
func (p *FileProvider) path(key string) (string, error) {
	if key == "" {
		return "", errors.New("secrets file: key required")
	}
	if strings.Contains(key, "..") || strings.HasPrefix(key, "/") || strings.HasPrefix(key, `\`) {
		return "", fmt.Errorf("secrets file: rejected key %q (path traversal not allowed)", key)
	}
	return filepath.Join(p.BaseDir, key), nil
}
