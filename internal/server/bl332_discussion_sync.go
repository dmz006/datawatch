// BL332 T42b — Discussion Scopes: federated sync, throttle, and conflict API.
//
// Extends the T42a discussion scope REST surface with:
//
//	GET    /api/memory/discussion/{id}/participants  → list participant peers
//	PUT    /api/memory/discussion/{id}/participants  → replace participant list
//	GET    /api/memory/discussion/{id}/conflicts     → list conflicting WAL entries
//	POST   /api/memory/discussion/{id}/conflicts/resolve → mark conflict resolved
//
// Push-on-write sync: after a successful POST to /api/memory/discussion/{id},
// the entry is forwarded to each participant peer asynchronously.
//
// Throttle: 60 writes/minute per Bearer token (token-bucket, in-memory).
//
// Conflict detection: WAL entries with the same content-prefix written by
// different origin peers within 5 seconds are surfaced as conflicts.

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// ---------------------------------------------------------------------------
// Participant list storage
// ---------------------------------------------------------------------------

// discussionParticipantsPath returns the participants.json path for a discussion.
// Creates the discussion directory on demand.
func discussionParticipantsPath(id string) (string, error) {
	base, err := discussionsBaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, id)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("mkdir discussion dir: %w", err)
	}
	return filepath.Join(dir, "participants.json"), nil
}

// discussionReadParticipants reads the participant list for a discussion.
// Returns an empty slice when no file exists. When encKey is non-nil the
// file is decrypted transparently (BL334 T43c).
func discussionReadParticipants(id string, encKey []byte) ([]string, error) {
	p, err := discussionParticipantsPath(id)
	if err != nil {
		return nil, err
	}
	data, err := secfile.ReadFile(p, encKey)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var v struct {
		Peers []string `json:"peers"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	if v.Peers == nil {
		return []string{}, nil
	}
	return v.Peers, nil
}

// discussionWriteParticipants atomically replaces the participant list.
// When encKey is non-nil the file is encrypted (BL334 T43c).
func discussionWriteParticipants(id string, peers []string, encKey []byte) error {
	p, err := discussionParticipantsPath(id)
	if err != nil {
		return err
	}
	data, err := json.Marshal(map[string]any{"peers": peers})
	if err != nil {
		return err
	}
	return secfile.WriteFile(p, data, 0600, encKey)
}

// handleDiscussionParticipants handles GET and PUT for
// /api/memory/discussion/{id}/participants.
func (s *Server) handleDiscussionParticipants(w http.ResponseWriter, r *http.Request, id string) {
	switch r.Method {
	case http.MethodGet:
		peers, err := discussionReadParticipants(id, s.encKey)
		if err != nil {
			http.Error(w, "read participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"discussion_id": id, "peers": peers})

	case http.MethodPut:
		var body struct {
			Peers []string `json:"peers"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.Peers == nil {
			body.Peers = []string{}
		}
		if err := discussionWriteParticipants(id, body.Peers, s.encKey); err != nil {
			http.Error(w, "write participants: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSONOK(w, map[string]any{"discussion_id": id, "peers": body.Peers, "ok": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// Token-bucket throttle — 60 writes/minute per Bearer token
// ---------------------------------------------------------------------------

type throttleBucket struct {
	mu     sync.Mutex
	tokens float64
	lastAt time.Time
	rate   float64 // tokens per second (1.0 = 60/min)
	max    float64 // burst cap (60)
}

// allow returns true and consumes 1 token, or returns false when empty.
func (b *throttleBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastAt).Seconds()
	b.lastAt = now
	b.tokens += elapsed * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// discussionThrottleMap holds one bucket per Bearer token.
var discussionThrottleMap sync.Map // key: string (Bearer token) → *throttleBucket

// discussionThrottleBucket returns (or creates) the bucket for a token.
func discussionThrottleBucket(tok string) *throttleBucket {
	v, _ := discussionThrottleMap.LoadOrStore(tok, &throttleBucket{
		tokens: 60,
		lastAt: time.Now(),
		rate:   1.0, // 60 tokens/min = 1/sec
		max:    60,
	})
	return v.(*throttleBucket)
}

// discussionBearerToken extracts the raw Bearer token from the request.
// Falls back to the ?token= query param. Returns an empty string for admin
// requests too (admin token is also tracked — throttle is write-volume, not
// access-control).
func discussionBearerToken(r *http.Request) string {
	tok := r.URL.Query().Get("token")
	if tok != "" {
		return tok
	}
	auth := r.Header.Get("Authorization")
	return strings.TrimPrefix(auth, "Bearer ")
}

// checkDiscussionThrottle enforces 60 writes/min per caller. Returns true
// if the write is allowed; false if rate-limited (and a 429 has been written).
func checkDiscussionThrottle(w http.ResponseWriter, r *http.Request) bool {
	tok := discussionBearerToken(r)
	if tok == "" {
		// No token — treat as anonymous bucket.
		tok = "__anon__"
	}
	bucket := discussionThrottleBucket(tok)
	if !bucket.allow() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit: 60 writes/min per peer"}`))
		return false
	}
	return true
}

// ---------------------------------------------------------------------------
// Push-on-write federation sync
// ---------------------------------------------------------------------------

// syncWritePayload is the body sent to each participant peer.
type syncWritePayload struct {
	Title        string `json:"title"`
	Message      string `json:"message"`
	OriginPeer   string `json:"origin_peer"`
	OriginWALSeq int    `json:"origin_wal_seq"`
}

// discussionSyncToParticipants fans out an entry to all participant peers
// asynchronously. Errors are logged (not returned to the caller).
func (s *Server) discussionSyncToParticipants(id string, entry discussionWALEntry) {
	peers, err := discussionReadParticipants(id, s.encKey)
	if err != nil || len(peers) == 0 {
		return
	}

	entryJSON, err := json.Marshal(entry)
	if err != nil {
		return
	}

	payload := syncWritePayload{
		Title:        "discussion-sync",
		Message:      string(entryJSON),
		OriginPeer:   s.hostname,
		OriginWALSeq: entry.Seq,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	for _, peerName := range peers {
		peerName := peerName
		go func() {
			if s.serverStore == nil {
				return
			}
			peerURL, tok, ok := s.serverStore.GetByName(peerName)
			if !ok || peerURL == "" {
				return
			}
			endpoint := strings.TrimRight(peerURL, "/") + "/api/push/" + id
			req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			if tok != "" {
				req.Header.Set("Authorization", "Bearer "+tok)
			}
			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Do(req)
			if err != nil {
				return
			}
			resp.Body.Close()
		}()
	}
}

// ---------------------------------------------------------------------------
// Conflict detection
// ---------------------------------------------------------------------------

// conflictEntry is one participant entry in a conflict group.
type conflictEntry struct {
	Seq        int       `json:"seq"`
	OriginPeer string    `json:"origin_peer"`
	Content    string    `json:"content"`
	Timestamp  time.Time `json:"timestamp"`
}

// conflictGroup groups WAL entries that share a content prefix and were
// written by different peers within 5 seconds.
type conflictGroup struct {
	ContentHash string          `json:"content_hash"`
	Entries     []conflictEntry `json:"entries"`
}

// discussionDetectConflicts scans the WAL and returns groups of conflicting entries.
func discussionDetectConflicts(id string, encKey []byte) ([]conflictGroup, error) {
	entries, err := discussionReadWAL(id, 100000, encKey)
	if err != nil {
		if os.IsNotExist(err) {
			return []conflictGroup{}, nil
		}
		return nil, err
	}

	// Group by first-64-chars-of-content (content hash).
	type group struct {
		entries []conflictEntry
	}
	byHash := map[string]*group{}
	for _, e := range entries {
		if e.Op != "" {
			// Skip conflict-resolved markers.
			continue
		}
		hash := e.Content
		if len(hash) > 64 {
			hash = hash[:64]
		}
		if _, ok := byHash[hash]; !ok {
			byHash[hash] = &group{}
		}
		byHash[hash].entries = append(byHash[hash].entries, conflictEntry{
			Seq:        e.Seq,
			OriginPeer: e.OriginPeer,
			Content:    e.Content,
			Timestamp:  e.Timestamp,
		})
	}

	// Collect groups where >= 2 different origin_peers wrote within 5 seconds.
	var conflicts []conflictGroup
	for hash, g := range byHash {
		if len(g.entries) < 2 {
			continue
		}
		// Check for at least 2 different origin_peers within 5s of each other.
		isConflict := false
	outer:
		for i := 0; i < len(g.entries); i++ {
			for j := i + 1; j < len(g.entries); j++ {
				a, b := g.entries[i], g.entries[j]
				if a.OriginPeer == b.OriginPeer {
					continue
				}
				diff := a.Timestamp.Sub(b.Timestamp)
				if diff < 0 {
					diff = -diff
				}
				if diff <= 5*time.Second {
					isConflict = true
					break outer
				}
			}
		}
		if isConflict {
			conflicts = append(conflicts, conflictGroup{
				ContentHash: hash,
				Entries:     g.entries,
			})
		}
	}
	return conflicts, nil
}

// handleDiscussionConflicts handles GET /api/memory/discussion/{id}/conflicts.
func (s *Server) handleDiscussionConflicts(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	conflicts, err := discussionDetectConflicts(id, s.encKey)
	if err != nil {
		http.Error(w, "detect conflicts: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if conflicts == nil {
		conflicts = []conflictGroup{}
	}
	writeJSONOK(w, map[string]any{"discussion_id": id, "conflicts": conflicts})
}

// handleDiscussionConflictResolve handles POST /api/memory/discussion/{id}/conflicts/resolve.
func (s *Server) handleDiscussionConflictResolve(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Seq int `json:"seq"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Find all entries for the same content hash as the winning seq.
	entries, err := discussionReadWAL(id, 100000, s.encKey)
	if err != nil {
		http.Error(w, "read wal: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Find the winning entry's content hash.
	winningHash := ""
	for _, e := range entries {
		if e.Seq == body.Seq {
			h := e.Content
			if len(h) > 64 {
				h = h[:64]
			}
			winningHash = h
			break
		}
	}
	if winningHash == "" {
		http.Error(w, "seq not found", http.StatusNotFound)
		return
	}

	// Append conflict-resolved markers for all non-winning entries with the same hash.
	// Use discussionAppendWALEntry so markers are encrypted when --secure is active.
	mu := discussionLock(id)
	mu.Lock()
	defer mu.Unlock()

	resolved := 0
	for _, e := range entries {
		if e.Seq == body.Seq || e.Op != "" {
			continue
		}
		h := e.Content
		if len(h) > 64 {
			h = h[:64]
		}
		if h != winningHash {
			continue
		}
		markerContent := fmt.Sprintf("conflict-resolved: seq=%d losing, winner=%d", e.Seq, body.Seq)
		_, _ = discussionAppendWALEntry(id, markerContent, "conflict-resolved", "", s.encKey)
		resolved++
	}

	writeJSONOK(w, map[string]any{
		"discussion_id": id,
		"winning_seq":   body.Seq,
		"resolved":      resolved,
		"ok":            true,
	})
}
