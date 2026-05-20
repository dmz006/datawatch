// BL332 T42a — Discussion Scopes: per-discussion federated shared memory.
// BL332 T42b — Federated sync, throttle (60 writes/min), and conflict API.
//
// REST surface:
//
//	GET    /api/memory/discussion                         → list all discussion scope IDs
//	GET    /api/memory/discussion/{id}                   → recall entries (query: ?q=&top_k=10)
//	POST   /api/memory/discussion/{id}                   → write entry; body: {content, summary?, role?}
//	DELETE /api/memory/discussion/{id}                   → delete ALL entries in this discussion scope
//	GET    /api/memory/discussion/{id}/wal               → last N WAL entries
//	GET    /api/memory/discussion/{id}/participants      → list participant peers (T42b)
//	PUT    /api/memory/discussion/{id}/participants      → replace participant list (T42b)
//	GET    /api/memory/discussion/{id}/conflicts         → list conflicting WAL entries (T42b)
//	POST   /api/memory/discussion/{id}/conflicts/resolve → mark conflict resolved (T42b)
//
// WAL path: ~/.datawatch/discussions/<id>/wal.jsonl
// Participants: ~/.datawatch/discussions/<id>/participants.json
// Discussion IDs are derived from the URL path segment after /api/memory/discussion/.

package server

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/federation"
	"github.com/dmz006/datawatch/internal/memory"
	"golang.org/x/crypto/chacha20poly1305"
)

// discussionWALEntry is one WAL record for a discussion scope write.
type discussionWALEntry struct {
	Seq        int       `json:"seq"`
	Content    string    `json:"content"`
	Role       string    `json:"role"`
	Timestamp  time.Time `json:"timestamp"`
	OriginPeer string    `json:"origin_peer,omitempty"`
	// Op is non-empty for administrative WAL records (e.g. "conflict-resolved").
	Op string `json:"op,omitempty"`
}

// discussionMu provides per-discussion write serialization.
// Key is discussion ID (string), value is *sync.Mutex.
var discussionMu sync.Map

// discussionLock returns (or creates) the per-discussion write mutex.
func discussionLock(id string) *sync.Mutex {
	v, _ := discussionMu.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// ---------------------------------------------------------------------------
// BL334 T43c — per-WAL-line encryption helpers
// ---------------------------------------------------------------------------

// walEncryptLine encrypts a JSON blob as "ENC:<base64(nonce24+ciphertext)>".
// Produces a single token with no embedded newlines, safe to write as a JSONL line.
func walEncryptLine(data, key []byte) (string, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, 24)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nonce, nonce, data, nil) // prepend nonce → nonce || ciphertext
	return "ENC:" + base64.StdEncoding.EncodeToString(ct), nil
}

// walDecryptLine decrypts an "ENC:…" WAL line; returns the raw JSON bytes
// for lines that were written before encryption was enabled (no ENC: prefix).
func walDecryptLine(line string, key []byte) ([]byte, error) {
	if !strings.HasPrefix(line, "ENC:") {
		return []byte(line), nil // plaintext line — migration / no-secure compat
	}
	combined, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(line, "ENC:"))
	if err != nil {
		return nil, fmt.Errorf("wal decrypt: base64: %w", err)
	}
	if len(combined) < 25 { // 24-byte nonce + at least 1 byte ciphertext
		return nil, fmt.Errorf("wal decrypt: combined too short")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, err
	}
	pt, err := aead.Open(nil, combined[:24], combined[24:], nil)
	if err != nil {
		return nil, fmt.Errorf("wal decrypt: %w", err)
	}
	return pt, nil
}

// discussionsBaseDir returns ~/.datawatch/discussions, creating it on demand.
func discussionsBaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("discussions dir: %w", err)
	}
	d := filepath.Join(home, ".datawatch", "discussions")
	if err := os.MkdirAll(d, 0755); err != nil {
		return "", fmt.Errorf("mkdir discussions: %w", err)
	}
	return d, nil
}

// discussionWALPath returns the WAL file path for a discussion.
// Creates the directory if needed.
func discussionWALPath(id string) (string, error) {
	base, err := discussionsBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir discussion dir: %w", err)
	}
	return filepath.Join(dir, "wal.jsonl"), nil
}

// discussionRole returns the memory role string used for discussion scope rows.
func discussionRole(id string) string {
	return "discussion/" + id
}

// handleDiscussionScopeList handles GET /api/memory/discussion — lists all
// discussion IDs that have a WAL directory under ~/.datawatch/discussions/.
func (s *Server) handleDiscussionScopeList(w http.ResponseWriter, r *http.Request) {
	if !s.fedCap(w, r, federation.CapCommRead) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	base, err := discussionsBaseDir()
	if err != nil {
		// If we can't even get the base dir, return empty list gracefully.
		writeJSONOK(w, map[string]any{"discussions": []string{}, "count": 0})
		return
	}

	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONOK(w, map[string]any{"discussions": []string{}, "count": 0})
			return
		}
		http.Error(w, "list discussions: "+err.Error(), http.StatusInternalServerError)
		return
	}

	ids := []string{}
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	writeJSONOK(w, map[string]any{"discussions": ids, "count": len(ids)})
}

// handleDiscussionScope handles /api/memory/discussion/{id} and its sub-paths:
//   - /api/memory/discussion/{id}/wal
//   - /api/memory/discussion/{id}/participants       (T42b)
//   - /api/memory/discussion/{id}/conflicts          (T42b)
//   - /api/memory/discussion/{id}/conflicts/resolve  (T42b)
func (s *Server) handleDiscussionScope(w http.ResponseWriter, r *http.Request) {
	// Extract the path suffix after /api/memory/discussion/
	rest := strings.TrimPrefix(r.URL.Path, "/api/memory/discussion/")
	rest = strings.Trim(rest, "/")

	if rest == "" {
		http.Error(w, "discussion id required", http.StatusBadRequest)
		return
	}

	// Check for /conflicts/resolve sub-path first (most specific).
	if idx := strings.Index(rest, "/conflicts/resolve"); idx >= 0 && rest[idx:] == "/conflicts/resolve" {
		id := rest[:idx]
		if id == "" {
			http.Error(w, "discussion id required", http.StatusBadRequest)
			return
		}
		if !s.fedCap(w, r, federation.CapCommWrite) {
			return
		}
		s.handleDiscussionConflictResolve(w, r, id)
		return
	}

	// Check for /conflicts sub-path.
	if idx := strings.Index(rest, "/conflicts"); idx >= 0 && rest[idx:] == "/conflicts" {
		id := rest[:idx]
		if id == "" {
			http.Error(w, "discussion id required", http.StatusBadRequest)
			return
		}
		if !s.fedCap(w, r, federation.CapCommRead) {
			return
		}
		s.handleDiscussionConflicts(w, r, id)
		return
	}

	// Check for /participants sub-path (T42b).
	if idx := strings.Index(rest, "/participants"); idx >= 0 && rest[idx:] == "/participants" {
		id := rest[:idx]
		if id == "" {
			http.Error(w, "discussion id required", http.StatusBadRequest)
			return
		}
		if r.Method == http.MethodGet {
			if !s.fedCap(w, r, federation.CapCommRead) {
				return
			}
		} else {
			if !s.fedCap(w, r, federation.CapCommWrite) {
				return
			}
		}
		s.handleDiscussionParticipants(w, r, id)
		return
	}

	// Check for /wal sub-path.
	if idx := strings.LastIndex(rest, "/wal"); idx >= 0 && rest[idx:] == "/wal" {
		id := rest[:idx]
		if id == "" {
			http.Error(w, "discussion id required", http.StatusBadRequest)
			return
		}
		if !s.fedCap(w, r, federation.CapCommRead) {
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleDiscussionWAL(w, r, id)
		return
	}

	// The rest of the path is the discussion ID.
	id := rest

	switch r.Method {
	case http.MethodGet:
		if !s.fedCap(w, r, federation.CapCommRead) {
			return
		}
		s.handleDiscussionRecall(w, r, id)
	case http.MethodPost:
		if !s.fedCap(w, r, federation.CapCommWrite) {
			return
		}
		s.handleDiscussionWrite(w, r, id)
	case http.MethodDelete:
		if !s.fedCap(w, r, federation.CapCommWrite) {
			return
		}
		s.handleDiscussionDelete(w, r, id)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDiscussionRecall handles GET /api/memory/discussion/{id}.
func (s *Server) handleDiscussionRecall(w http.ResponseWriter, r *http.Request, id string) {
	if s.memoryBackend == nil {
		writeJSONOK(w, map[string]any{"results": []any{}, "count": 0, "discussion_id": id})
		return
	}

	q := r.URL.Query()
	topK := atoiDefault(q.Get("top_k"), 10)
	role := discussionRole(id)

	// List entries by role from the discussion scope.
	hits, err := s.memoryBackend.ListByRole("", role, topK)
	if err != nil {
		http.Error(w, "recall: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"discussion_id": id,
		"results":       hits,
		"count":         len(hits),
	})
}

// handleDiscussionWrite handles POST /api/memory/discussion/{id}.
func (s *Server) handleDiscussionWrite(w http.ResponseWriter, r *http.Request, id string) {
	// T42b: throttle — 60 writes/minute per Bearer token.
	if !checkDiscussionThrottle(w, r) {
		return
	}

	var body struct {
		Content      string `json:"content"`
		Summary      string `json:"summary"`
		Role         string `json:"role"`
		// T42b: loop-prevention fields set by peer syncs.
		OriginPeer   string `json:"origin_peer,omitempty"`
		OriginWALSeq int    `json:"origin_wal_seq,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Content == "" {
		http.Error(w, "content required", http.StatusBadRequest)
		return
	}
	if body.Role == "" {
		body.Role = "discussion"
	}

	// T42b: loop prevention — skip sync if we are the origin.
	isSyncLoop := body.OriginPeer != "" && body.OriginPeer == s.hostname

	mu := discussionLock(id)
	mu.Lock()
	defer mu.Unlock()

	var memID int64
	if s.memoryBackend != nil {
		var err error
		memID, err = s.memoryBackend.Save("", body.Content, body.Summary, discussionRole(id), "", nil)
		if err != nil {
			http.Error(w, "save: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Append to WAL and capture the entry for sync.
	walEntry, err := discussionAppendWALEntry(id, body.Content, body.Role, body.OriginPeer, s.encKey)
	if err != nil {
		// WAL failure is non-fatal.
		_ = err
	}

	// T42b: push-on-write sync to participant peers (skip if this write
	// originated from a peer — loop prevention).
	if !isSyncLoop && walEntry != nil {
		s.discussionSyncToParticipants(id, *walEntry)
	}

	writeJSONOK(w, map[string]any{
		"discussion_id": id,
		"memory_id":     memID,
		"ok":            true,
	})
}

// handleDiscussionDelete handles DELETE /api/memory/discussion/{id}.
func (s *Server) handleDiscussionDelete(w http.ResponseWriter, r *http.Request, id string) {
	mu := discussionLock(id)
	mu.Lock()
	defer mu.Unlock()

	deleted := 0
	if s.memoryBackend != nil {
		role := discussionRole(id)
		// List all entries for this discussion scope.
		entries, err := s.memoryBackend.ListByRole("", role, 10000)
		if err != nil {
			http.Error(w, "list: "+err.Error(), http.StatusInternalServerError)
			return
		}
		for _, m := range entries {
			if delErr := s.memoryBackend.Delete(m.ID); delErr == nil {
				deleted++
			}
		}
	}

	// Remove WAL directory.
	base, err := discussionsBaseDir()
	if err == nil {
		dir := filepath.Join(base, id)
		os.RemoveAll(dir) //nolint:errcheck
	}

	writeJSONOK(w, map[string]any{
		"discussion_id": id,
		"deleted":       deleted,
		"ok":            true,
	})
}

// handleDiscussionWAL handles GET /api/memory/discussion/{id}/wal.
func (s *Server) handleDiscussionWAL(w http.ResponseWriter, r *http.Request, id string) {
	q := r.URL.Query()
	n := atoiDefault(q.Get("n"), 50)

	entries, err := discussionReadWAL(id, n, s.encKey)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSONOK(w, map[string]any{"discussion_id": id, "entries": []any{}, "count": 0})
			return
		}
		http.Error(w, "read wal: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSONOK(w, map[string]any{
		"discussion_id": id,
		"entries":       entries,
		"count":         len(entries),
	})
}

// discussionAppendWALEntry appends one entry and returns it (for sync).
// originPeer may be empty for local writes. When encKey is non-nil each
// line is encrypted as "ENC:<base64(nonce24+ciphertext)>" (BL334 T43c).
func discussionAppendWALEntry(id, content, role, originPeer string, encKey []byte) (*discussionWALEntry, error) {
	walPath, err := discussionWALPath(id)
	if err != nil {
		return nil, err
	}

	// Compute next sequence number by counting existing lines.
	seq := 0
	if f, err := os.Open(walPath); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			seq++
		}
		f.Close()
	}

	entry := discussionWALEntry{
		Seq:        seq,
		Content:    content,
		Role:       role,
		Timestamp:  time.Now().UTC(),
		OriginPeer: originPeer,
	}
	jsonBytes, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}

	// Encrypt the line when --secure is active.
	lineStr := string(jsonBytes)
	if encKey != nil {
		enc, err := walEncryptLine(jsonBytes, encKey)
		if err != nil {
			return nil, fmt.Errorf("wal encrypt: %w", err)
		}
		lineStr = enc
	}

	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err = fmt.Fprintf(f, "%s\n", lineStr); err != nil {
		return nil, err
	}
	return &entry, nil
}

// discussionReadWAL reads the last n entries from the discussion WAL.
// When encKey is non-nil each line is decrypted before parsing (BL334 T43c).
// Lines without the "ENC:" prefix are accepted transparently for migration compat.
func discussionReadWAL(id string, n int, encKey []byte) ([]discussionWALEntry, error) {
	walPath, err := discussionWALPath(id)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(walPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var all []discussionWALEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20) // 1 MB max line (encrypted lines are larger)
	for sc.Scan() {
		raw := sc.Text()
		if encKey != nil {
			dec, err := walDecryptLine(raw, encKey)
			if err != nil {
				continue // skip corrupted/unreadable lines
			}
			raw = string(dec)
		}
		var e discussionWALEntry
		if err := json.Unmarshal([]byte(raw), &e); err == nil {
			all = append(all, e)
		}
	}

	// Return the last n entries.
	if len(all) <= n {
		return all, nil
	}
	return all[len(all)-n:], nil
}

// Ensure memory.ScopeDiscussion is referenced to avoid dead-code stripping.
var _ = memory.ScopeDiscussion
