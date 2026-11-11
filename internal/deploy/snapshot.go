package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Snapshot describes a copied binary.
type Snapshot struct {
	Path string
	Hash string
	Time time.Time
}

// takeSnapshot copies src to dst, keeping src's mode, and returns the SHA-256
// of the copy. dst must lie inside dataDir. A missing src is an error and
// nothing is written.
func takeSnapshot(src, dst, dataDir string, now time.Time) (Snapshot, error) {
	if dataDir == "" {
		return Snapshot{}, fmt.Errorf("no daemon data directory for the snapshot")
	}
	rel, err := filepath.Rel(filepath.Clean(dataDir), filepath.Clean(dst))
	if err != nil || rel == "." || rel == ".." || len(rel) > 2 && rel[:3] == "../" {
		return Snapshot{}, fmt.Errorf("snapshot path is outside the data directory")
	}
	in, err := os.Open(src) // #nosec G304 -- operator's binary_path
	if err != nil {
		return Snapshot{}, fmt.Errorf("binary_path is not readable: %w", err)
	}
	defer func() { _ = in.Close() }()
	fi, err := in.Stat()
	if err != nil {
		return Snapshot{}, err
	}
	if !fi.Mode().IsRegular() {
		return Snapshot{}, fmt.Errorf("binary_path is not a regular file")
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return Snapshot{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".snapshot-*")
	if err != nil {
		return Snapshot{}, err
	}
	name := tmp.Name()
	fail := func(e error) (Snapshot, error) {
		_ = tmp.Close()
		_ = os.Remove(name)
		return Snapshot{}, e
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(fi.Mode().Perm()); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return Snapshot{}, err
	}
	if err := os.Rename(name, dst); err != nil {
		_ = os.Remove(name)
		return Snapshot{}, err
	}
	return Snapshot{Path: dst, Hash: hex.EncodeToString(h.Sum(nil)), Time: now}, nil
}
