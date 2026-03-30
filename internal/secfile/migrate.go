package secfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const migrateSentinel = ".enc.migrated"

// MigratePlaintextToEncrypted scans dataDir for plaintext output.log and
// tracker .md files, encrypts them in place. A sentinel file prevents
// re-migration on subsequent starts.
//
// When trackingFull is false (default), only output.log files are encrypted.
// When trackingFull is true, tracker .md files are also encrypted.
func MigratePlaintextToEncrypted(dataDir string, key []byte, trackingFull bool) error {
	sentinelPath := filepath.Join(dataDir, migrateSentinel)
	if _, err := os.Stat(sentinelPath); err == nil {
		return nil // already migrated
	}

	sessionsDir := filepath.Join(dataDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return writeSentinel(sentinelPath)
		}
		return fmt.Errorf("migrate: read sessions dir: %w", err)
	}

	migrated := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessDir := filepath.Join(sessionsDir, e.Name())

		// Encrypt output.log
		logPath := filepath.Join(sessDir, "output.log")
		if n, err := migrateFile(logPath, key); err != nil {
			fmt.Printf("[migrate] warn: %s: %v\n", logPath, err)
		} else {
			migrated += n
		}

		// Encrypt tracker .md files if full mode
		if trackingFull {
			trackDir := filepath.Join(sessDir, "tracking")
			mdFiles, _ := filepath.Glob(filepath.Join(trackDir, "*.md"))
			for _, md := range mdFiles {
				if n, err := migrateFile(md, key); err != nil {
					fmt.Printf("[migrate] warn: %s: %v\n", md, err)
				} else {
					migrated += n
				}
			}
		}
	}

	if migrated > 0 {
		fmt.Printf("[migrate] Encrypted %d plaintext files\n", migrated)
	}
	return writeSentinel(sentinelPath)
}

// migrateFile encrypts a single file in place if it's plaintext.
// Returns 1 if migrated, 0 if already encrypted or not found.
func migrateFile(path string, key []byte) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}
	// Skip if already encrypted (DWDAT1/DWDAT2/DWLOG1)
	s := string(data)
	if strings.HasPrefix(s, "DWDAT1\n") || strings.HasPrefix(s, "DWDAT2\n") || strings.HasPrefix(s, "DWLOG1\n") {
		return 0, nil
	}

	// Encrypt with XChaCha20-Poly1305
	enc, err := Encrypt(data, key)
	if err != nil {
		return 0, fmt.Errorf("encrypt: %w", err)
	}
	if err := os.WriteFile(path, enc, 0600); err != nil {
		return 0, fmt.Errorf("write: %w", err)
	}
	return 1, nil
}

func writeSentinel(path string) error {
	return os.WriteFile(path, []byte("migrated\n"), 0600)
}

// IsMigrated returns true if the data directory has been migrated.
func IsMigrated(dataDir string) bool {
	_, err := os.Stat(filepath.Join(dataDir, migrateSentinel))
	return err == nil
}
