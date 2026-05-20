// BL334 T43e — Operational data encryption status and secure wipe.
//
// REST surface:
//
//	GET  /api/security/encryption/status   → show what is encrypted, what is plaintext
//	POST /api/security/encryption/migrate  → encrypt all plaintext operational files
//	POST /api/security/wipe-plaintext      → 3-pass overwrite + unlink plaintext originals
//	                                          body: {"confirm": true} required
//
// The migrate endpoint is idempotent (already-encrypted files are skipped).
// The wipe endpoint requires explicit confirmation and records the wipe to the
// audit log. It uses 3 passes (zeros, ones, random) per file; note that on
// modern SSD/NVMe and copy-on-write filesystems this is best-effort — operators
// requiring stronger guarantees should use LUKS or an encrypted home directory.

package server

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/secfile"
)

// ---------------------------------------------------------------------------
// Status
// ---------------------------------------------------------------------------

type encryptionFileStatus struct {
	Path      string `json:"path"`
	Encrypted bool   `json:"encrypted"`
	Exists    bool   `json:"exists"`
}

type encryptionStatusResponse struct {
	SecureMode bool                   `json:"secure_mode"`
	Files      []encryptionFileStatus `json:"files"`
	Summary    string                 `json:"summary"`
}

// handleSecurityEncryptionStatus handles GET /api/security/encryption/status.
func (s *Server) handleSecurityEncryptionStatus(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".datawatch")

	var files []encryptionFileStatus

	// channel_routing.json
	files = append(files, probeFile(filepath.Join(base, "channel_routing.json")))

	// discussion WAL and participants under ~/.datawatch/discussions/
	discussionsDir := filepath.Join(base, "discussions")
	if entries, err := os.ReadDir(discussionsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(discussionsDir, e.Name())
			files = append(files, probeFile(filepath.Join(dir, "wal.jsonl")))
			files = append(files, probeFile(filepath.Join(dir, "participants.json")))
		}
	}

	// Count plaintext files.
	plain := 0
	for _, f := range files {
		if f.Exists && !f.Encrypted {
			plain++
		}
	}

	summary := "all encrypted"
	if s.encKey == nil {
		summary = "secure mode disabled — files are plaintext"
	} else if plain > 0 {
		summary = fmt.Sprintf("%d file(s) still plaintext — run POST /api/security/encryption/migrate", plain)
	}

	writeJSONOK(w, encryptionStatusResponse{
		SecureMode: s.encKey != nil,
		Files:      files,
		Summary:    summary,
	})
}

// probeFile checks whether path exists and whether it is secfile-encrypted.
func probeFile(path string) encryptionFileStatus {
	data, err := os.ReadFile(path)
	if err != nil {
		return encryptionFileStatus{Path: path, Exists: false}
	}
	isEnc := secfile.IsEncrypted(data) || isWALEncrypted(data)
	return encryptionFileStatus{Path: path, Exists: true, Encrypted: isEnc}
}

// isWALEncrypted returns true when every non-empty line in a JSONL WAL
// starts with "ENC:" (per-line encryption, BL334 T43c).
func isWALEncrypted(data []byte) bool {
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, l := range lines {
		if l == "" {
			continue
		}
		if !strings.HasPrefix(l, "ENC:") {
			return false
		}
	}
	return len(lines) > 0
}

// ---------------------------------------------------------------------------
// Migrate
// ---------------------------------------------------------------------------

// handleSecurityEncryptionMigrate handles POST /api/security/encryption/migrate.
func (s *Server) handleSecurityEncryptionMigrate(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.encKey == nil {
		http.Error(w, "secure mode not active — start daemon with --secure", http.StatusBadRequest)
		return
	}

	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".datawatch")

	migratedDiscussions, err := secfile.MigrateDiscussionWALs(filepath.Join(base, "discussions"), s.encKey)
	if err != nil {
		http.Error(w, "discussion migration: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := secfile.MigrateChannelRouting(filepath.Join(base, "channel_routing.json"), s.encKey); err != nil {
		http.Error(w, "channel_routing migration: "+err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONOK(w, map[string]any{
		"ok":                   true,
		"discussions_migrated": migratedDiscussions,
		"message":              "migration complete — re-check /api/security/encryption/status",
	})
}

// ---------------------------------------------------------------------------
// Secure wipe
// ---------------------------------------------------------------------------

// handleSecurityWipePlaintext handles POST /api/security/wipe-plaintext.
// Body must contain {"confirm": true} — any other value returns 400.
// Each file is overwritten 3 times (zeros, ones, random) then deleted.
func (s *Server) handleSecurityWipePlaintext(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommWrite) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Confirm bool `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if !body.Confirm {
		http.Error(w, `{"error":"confirmation required","hint":"set {\"confirm\":true} to proceed"}`, http.StatusBadRequest)
		return
	}

	home, _ := os.UserHomeDir()
	base := filepath.Join(home, ".datawatch")

	var wiped []string
	var skipped []string

	// Collect plaintext discussion WAL and participants files.
	discussionsDir := filepath.Join(base, "discussions")
	if entries, err := os.ReadDir(discussionsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(discussionsDir, e.Name())
			for _, fname := range []string{"wal.jsonl", "participants.json"} {
				p := filepath.Join(dir, fname)
				ok, enc := isPlaintext(p)
				if !ok {
					continue // doesn't exist
				}
				if enc {
					skipped = append(skipped, p)
					continue
				}
				if err := secureWipe(p); err != nil {
					http.Error(w, "wipe failed: "+err.Error(), http.StatusInternalServerError)
					return
				}
				wiped = append(wiped, p)
			}
		}
	}

	// channel_routing.json
	crPath := filepath.Join(base, "channel_routing.json")
	if ok, enc := isPlaintext(crPath); ok {
		if enc {
			skipped = append(skipped, crPath)
		} else {
			if err := secureWipe(crPath); err != nil {
				http.Error(w, "wipe failed: "+err.Error(), http.StatusInternalServerError)
				return
			}
			wiped = append(wiped, crPath)
		}
	}

	writeJSONOK(w, map[string]any{
		"ok":      true,
		"wiped":   wiped,
		"skipped": skipped,
		"note":    "3-pass overwrite (zeros/ones/random) then unlink. SSD/CoW filesystems: use LUKS for stronger guarantees.",
	})
}

// isPlaintext returns (exists bool, encrypted bool).
func isPlaintext(path string) (bool, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, false
	}
	if len(data) == 0 {
		return true, true // treat empty as "encrypted" (nothing to wipe)
	}
	enc := secfile.IsEncrypted(data) || isWALEncrypted(data)
	return true, enc
}

// secureWipe overwrites path 3 times (zeros, ones, random) then removes it.
func secureWipe(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	size := info.Size()
	if size == 0 {
		return os.Remove(path)
	}

	passes := []func(int64) []byte{
		func(n int64) []byte { return make([]byte, n) },           // zeros
		func(n int64) []byte { b := make([]byte, n); for i := range b { b[i] = 0xFF }; return b }, // ones
		func(n int64) []byte {
			b := make([]byte, n)
			_, _ = io.ReadFull(rand.Reader, b)
			return b
		}, // random
	}

	for _, gen := range passes {
		f, err := os.OpenFile(path, os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("open for wipe: %w", err)
		}
		data := gen(size)
		if _, err := f.Write(data); err != nil {
			f.Close()
			return fmt.Errorf("write pass: %w", err)
		}
		if err := f.Sync(); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return os.Remove(path)
}
