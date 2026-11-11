package packs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Remove deletes the installed copy of one pack version from installRoot and
// drops its trust record from trust.json in the same root. The installed folder
// is installRoot/<name>/<version>/.
//
// Remove touches nothing else. Automata, their resolved task lists, tasks and
// trace records are not read or written: an Automaton keeps the tasks it was
// created with, so removing a pack never changes it.
//
// The trust record is dropped before the folder is deleted. If the folder
// deletion then fails, the pack is left on disk but no longer runnable, which is
// the safe side of a partial failure.
func Remove(name, version, installRoot string) error {
	if err := checkInstallName(name); err != nil || name == "" {
		return fmt.Errorf("pack remove: name %q is not a safe folder name", name)
	}
	if err := checkInstallName(version); err != nil || version == "" {
		return fmt.Errorf("pack remove: version %q is not a safe folder name", version)
	}
	absRoot, err := filepath.Abs(installRoot)
	if err != nil {
		return fmt.Errorf("pack remove: %w", err)
	}
	dir := filepath.Join(absRoot, name, version)
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("pack remove: %s %s is not installed", name, version)
	}
	if err != nil {
		return fmt.Errorf("pack remove: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("pack remove: %s is not an installed pack folder", dir)
	}

	store, err := LoadTrustStore(absRoot)
	if err != nil {
		return fmt.Errorf("pack remove: %w", err)
	}
	if err := store.forget(name, version); err != nil {
		return fmt.Errorf("pack remove: %w", err)
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("pack remove: %w", err)
	}
	// Drop the name folder when this was its last installed version. A
	// non-empty folder is left alone, and so is any error from the attempt.
	_ = os.Remove(filepath.Join(absRoot, name))
	return nil
}

// forget drops every trust record for name and version and writes the store
// back to disk. It writes nothing when no record matches, so a pack that was
// never accepted leaves trust.json untouched.
func (s *TrustStore) forget(name, version string) error {
	key := recordKey(name, version)
	kept := make([]TrustRecord, 0, len(s.records))
	for _, r := range s.records {
		if recordKey(r.Name, r.Version) == key {
			continue
		}
		kept = append(kept, r)
	}
	if len(kept) == len(s.records) {
		return nil
	}
	if err := writeTrustFile(s.path, kept); err != nil {
		return err
	}
	s.records = kept
	return nil
}
