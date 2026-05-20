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

// ---------------------------------------------------------------------------
// BL334 T43d — upgrade-compatible migration for operational data files
// ---------------------------------------------------------------------------

// MigrateDiscussionWALs encrypts all plaintext discussion WAL lines and
// participants.json files under discussionsDir. Safe to call multiple times
// (each file is migrated in-place; already-encrypted lines are skipped).
//
// WAL format: each line is re-written as "ENC:<base64(nonce24+ciphertext)>".
// Lines that already start with "ENC:" are left untouched (idempotent).
func MigrateDiscussionWALs(discussionsDir string, key []byte) (int, error) {
	entries, err := os.ReadDir(discussionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("migrate discussions: read dir: %w", err)
	}

	migrated := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		dir := filepath.Join(discussionsDir, id)

		// Migrate WAL lines.
		walPath := filepath.Join(dir, "wal.jsonl")
		if n, err := migrateWALLines(walPath, key); err != nil {
			fmt.Printf("[migrate] warn: discussion %s WAL: %v\n", id, err)
		} else {
			migrated += n
		}

		// Migrate participants.json.
		partsPath := filepath.Join(dir, "participants.json")
		if n, err := migrateFile(partsPath, key); err != nil {
			fmt.Printf("[migrate] warn: discussion %s participants: %v\n", id, err)
		} else {
			migrated += n
		}
	}

	if migrated > 0 {
		fmt.Printf("[migrate] Encrypted %d discussion file(s)\n", migrated)
	}
	return migrated, nil
}

// migrateWALLines re-encrypts a JSONL WAL file line-by-line. Lines already
// prefixed with "ENC:" are left unchanged. Returns 1 if any lines were
// encrypted, 0 if the file did not exist or was already fully encrypted.
func migrateWALLines(walPath string, key []byte) (int, error) {
	data, err := os.ReadFile(walPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	if len(data) == 0 {
		return 0, nil
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	changed := false
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "ENC:") {
			continue // blank or already encrypted
		}
		enc, err := walLineEncryptBytes([]byte(line), key)
		if err != nil {
			return 0, fmt.Errorf("encrypt line %d: %w", i, err)
		}
		lines[i] = enc
		changed = true
	}
	if !changed {
		return 0, nil
	}

	out := strings.Join(lines, "\n") + "\n"
	if err := WriteFile(walPath, []byte(out), 0600, nil); err != nil { // write raw (lines already encrypted)
		return 0, fmt.Errorf("write migrated WAL: %w", err)
	}
	return 1, nil
}

// walLineEncryptBytes produces "ENC:<base64(nonce24+ciphertext)>" from data,
// compatible with the WAL per-line format in bl332_discussion_scope.go.
func walLineEncryptBytes(data, key []byte) (string, error) {
	enc, err := Encrypt(data, key)
	if err != nil {
		return "", err
	}
	// Encrypt returns "DWDAT2\n<base64>\n" — strip the header/newline to get the
	// raw base64(nonce24+ciphertext) token used in the WAL "ENC:" line format.
	b64 := strings.TrimSpace(strings.TrimPrefix(string(enc), "DWDAT2\n"))
	return "ENC:" + b64, nil
}

// MigrateChannelRouting encrypts channel_routing.json if it exists and is
// not already encrypted. Uses secfile atomic write. No-op if the file is
// absent. Safe to call multiple times.
func MigrateChannelRouting(path string, key []byte) error {
	n, err := migrateFile(path, key)
	if err != nil {
		return fmt.Errorf("migrate channel_routing: %w", err)
	}
	if n > 0 {
		fmt.Printf("[migrate] Encrypted channel_routing.json\n")
	}
	return nil
}
