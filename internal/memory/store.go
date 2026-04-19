package memory

import (
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure Go SQLite driver — no cgo, no root needed
)

// Memory represents a single stored memory record.
type Memory struct {
	ID         int64     `json:"id"`
	SessionID  string    `json:"session_id,omitempty"`
	ProjectDir string    `json:"project_dir"`
	Content    string    `json:"content"`
	Summary    string    `json:"summary,omitempty"`
	Role       string    `json:"role"`       // "session", "manual", "learning", "output_chunk"
	Wing       string    `json:"wing,omitempty"`       // project/person grouping (BL55)
	Room       string    `json:"room,omitempty"`       // topic within a wing (BL55)
	Hall       string    `json:"hall,omitempty"`       // standardized type: facts/events/discoveries/preferences/advice (BL55)
	// Namespace isolates memories per F10 Project Profile so workers
	// only see their own writes (sprint 6 federation). Default
	// "__global__" preserves the pre-F10 single-namespace behaviour;
	// federated reads can union multiple namespaces. (F10 S6.1)
	Namespace  string    `json:"namespace,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	Similarity float64   `json:"similarity,omitempty"` // populated by search results
}

// DefaultNamespace is what Save / SaveWithMeta tag rows with when
// the caller doesn't supply one — preserves the pre-F10 single-
// namespace world for back-compat. F10 worker callers use
// SaveWithNamespace + Project Profile's namespace string.
const DefaultNamespace = "__global__"

// Store is the SQLite-backed memory store.
type Store struct {
	db      *sql.DB
	walPath string   // write-ahead log file path
	walMu   sync.Mutex
	encKey  []byte   // 32-byte XChaCha20 key; nil = no encryption
}

// NewStore opens or creates a SQLite memory database at the given path.
func NewStore(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open memory db: %w", err)
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate memory db: %w", err)
	}
	walPath := filepath.Join(filepath.Dir(dbPath), "memory-wal.jsonl")
	return &Store{db: db, walPath: walPath}, nil
}

// NewStoreEncrypted opens a memory store with content encryption enabled.
// The 32-byte key encrypts content and summary fields; embeddings and metadata
// remain unencrypted for search and filtering.
func NewStoreEncrypted(dbPath string, encKey []byte) (*Store, error) {
	s, err := NewStore(dbPath)
	if err != nil {
		return nil, err
	}
	if len(encKey) != 32 {
		s.Close()
		return nil, fmt.Errorf("memory encryption key must be 32 bytes, got %d", len(encKey))
	}
	s.encKey = make([]byte, 32)
	copy(s.encKey, encKey)
	return s, nil
}

// IsEncrypted returns true if the store has encryption enabled.
func (s *Store) IsEncrypted() bool { return len(s.encKey) == 32 }

// encryptField encrypts a string for storage. Returns original if no key set.
func (s *Store) encryptField(plaintext string) string {
	if len(s.encKey) != 32 || plaintext == "" {
		return plaintext
	}
	ct, err := fieldEncrypt([]byte(plaintext), s.encKey)
	if err != nil {
		return plaintext // fallback to plaintext on error
	}
	return "ENC:" + ct
}

// decryptMemory decrypts the content and summary fields of a memory in-place.
func (s *Store) decryptMemory(m *Memory) {
	m.Content = s.decryptField(m.Content)
	m.Summary = s.decryptField(m.Summary)
}

// decryptField decrypts a stored field. Returns original if not encrypted.
func (s *Store) decryptField(stored string) string {
	if len(s.encKey) != 32 || !strings.HasPrefix(stored, "ENC:") {
		return stored
	}
	pt, err := fieldDecrypt(stored[4:], s.encKey)
	if err != nil {
		return stored // return as-is on error
	}
	return string(pt)
}

func migrate(db *sql.DB) error {
	// Create table without content_hash first (compatible with existing DBs)
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS memories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT DEFAULT '',
			project_dir TEXT NOT NULL,
			content TEXT NOT NULL,
			summary TEXT DEFAULT '',
			role TEXT DEFAULT 'session',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			embedding BLOB
		);
		CREATE INDEX IF NOT EXISTS idx_memories_project ON memories(project_dir);
		CREATE INDEX IF NOT EXISTS idx_memories_role ON memories(role);
		CREATE INDEX IF NOT EXISTS idx_memories_session ON memories(session_id);
	`)
	if err != nil {
		return err
	}
	// Migration v1.4.0: add content_hash column for deduplication
	db.Exec(`ALTER TABLE memories ADD COLUMN content_hash TEXT DEFAULT ''`) //nolint:errcheck
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_hash ON memories(content_hash)`) //nolint:errcheck
	// Migration v1.5.0: add wing/room/hall columns for spatial organization (BL55)
	db.Exec(`ALTER TABLE memories ADD COLUMN wing TEXT DEFAULT ''`) //nolint:errcheck
	db.Exec(`ALTER TABLE memories ADD COLUMN room TEXT DEFAULT ''`) //nolint:errcheck
	db.Exec(`ALTER TABLE memories ADD COLUMN hall TEXT DEFAULT ''`) //nolint:errcheck
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_wing ON memories(wing)`) //nolint:errcheck
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_room ON memories(room)`) //nolint:errcheck
	// F10 S6.1: per-Project-Profile namespace for memory federation.
	// DEFAULT '__global__' so existing rows carry forward into the
	// global namespace + the pre-F10 single-namespace queries keep
	// matching them.
	db.Exec(`ALTER TABLE memories ADD COLUMN namespace TEXT NOT NULL DEFAULT '__global__'`) //nolint:errcheck
	db.Exec(`CREATE INDEX IF NOT EXISTS idx_memories_namespace ON memories(namespace)`) //nolint:errcheck
	// Backfill any nullable historical rows.
	db.Exec(`UPDATE memories SET namespace = '__global__' WHERE namespace IS NULL OR namespace = ''`) //nolint:errcheck
	return nil
}

// contentHash returns the SHA-256 hash of normalized content for dedup.
func contentHash(content string) string {
	normalized := strings.TrimSpace(strings.ToLower(content))
	h := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(h[:])
}

// FindDuplicate checks if a memory with identical content already exists for a project.
// Returns the existing memory ID, or 0 if no duplicate.
func (s *Store) FindDuplicate(projectDir, content string) int64 {
	hash := contentHash(content)
	var id int64
	err := s.db.QueryRow(
		`SELECT id FROM memories WHERE project_dir = ? AND content_hash = ? LIMIT 1`,
		projectDir, hash,
	).Scan(&id)
	if err != nil {
		return 0
	}
	return id
}

// Save stores a new memory with its embedding vector.
// Returns the existing ID if a duplicate is found (deduplication).
func (s *Store) Save(projectDir, content, summary, role, sessionID string, embedding []float32) (int64, error) {
	return s.SaveWithMeta(projectDir, content, summary, role, sessionID, "", "", "", embedding)
}

// SaveWithMeta stores a memory with spatial metadata (wing/room/hall)
// in the default namespace. Back-compat wrapper around
// SaveWithNamespace.
func (s *Store) SaveWithMeta(projectDir, content, summary, role, sessionID, wing, room, hall string, embedding []float32) (int64, error) {
	return s.SaveWithNamespace(DefaultNamespace, projectDir, content, summary, role, sessionID, wing, room, hall, embedding)
}

// SaveWithNamespace stores a memory tagged with the given namespace
// (F10 S6.1). Empty namespace defaults to DefaultNamespace.
func (s *Store) SaveWithNamespace(namespace, projectDir, content, summary, role, sessionID, wing, room, hall string, embedding []float32) (int64, error) {
	if namespace == "" {
		namespace = DefaultNamespace
	}

	// Dedup check
	if existingID := s.FindDuplicate(projectDir, content); existingID > 0 {
		return existingID, nil
	}

	// Auto-derive wing from project dir if not provided
	if wing == "" {
		wing = filepath.Base(projectDir)
	}
	// Auto-classify hall from role
	if hall == "" {
		switch role {
		case "manual":
			hall = "facts"
		case "session":
			hall = "events"
		case "learning":
			hall = "discoveries"
		case "output_chunk":
			hall = "events"
		}
	}

	var embBlob []byte
	if len(embedding) > 0 {
		embBlob = encodeVector(embedding)
	}
	hash := contentHash(content)
	// Encrypt content fields if encryption is enabled
	storedContent := s.encryptField(content)
	storedSummary := s.encryptField(summary)

	result, err := s.db.Exec(
		`INSERT INTO memories (session_id, project_dir, content, summary, role, embedding, content_hash, wing, room, hall, namespace)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sessionID, projectDir, storedContent, storedSummary, role, embBlob, hash, wing, room, hall, namespace,
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()

	// Write-ahead log
	s.walLog("save", map[string]interface{}{
		"id": id, "project_dir": projectDir, "role": role,
		"wing": wing, "room": room, "hall": hall,
		"namespace":   namespace,
		"content_len": len(content), "session_id": sessionID,
	})

	return id, nil
}

// SearchFiltered finds top-K memories with optional wing/room filtering.
// Filtering before cosine similarity dramatically improves retrieval accuracy.
func (s *Store) SearchFiltered(wing, room string, queryVec []float32, topK int) ([]Memory, error) {
	query := `SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at, embedding
		 FROM memories WHERE embedding IS NOT NULL`
	var args []interface{}
	if wing != "" {
		query += ` AND wing = ?`
		args = append(args, wing)
	}
	if room != "" {
		query += ` AND room = ?`
		args = append(args, room)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		Memory
		score float64
	}
	var candidates []scored

	for rows.Next() {
		var m Memory
		var embBlob []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
			&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt, &embBlob); err != nil {
			continue
		}
		if len(embBlob) == 0 {
			continue
		}
		vec := decodeVector(embBlob)
		sim := CosineSimilarity(queryVec, vec)
		s.decryptMemory(&m)
		candidates = append(candidates, scored{Memory: m, score: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}
	results := make([]Memory, topK)
	for i := 0; i < topK; i++ {
		results[i] = candidates[i].Memory
		results[i].Similarity = candidates[i].score
	}
	return results, nil
}

// ListRooms returns distinct rooms within a wing.
func (s *Store) ListRooms(wing string) ([]string, error) {
	rows, err := s.db.Query(
		`SELECT DISTINCT room FROM memories WHERE wing = ? AND room != '' ORDER BY room`, wing)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var r string
		rows.Scan(&r) //nolint:errcheck
		if r != "" {
			result = append(result, r)
		}
	}
	return result, nil
}

// ListWings returns distinct wings with memory counts.
func (s *Store) ListWings() (map[string]int, error) {
	rows, err := s.db.Query(
		`SELECT wing, COUNT(*) FROM memories WHERE wing != '' GROUP BY wing ORDER BY wing`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var w string
		var c int
		rows.Scan(&w, &c) //nolint:errcheck
		result[w] = c
	}
	return result, nil
}

// SearchInNamespaces finds the top-K most similar memories whose
// `namespace` is in the supplied set, by cosine similarity. This is
// the federated-read primitive used by Sprint 6 modes:
//   - shared:    namespaces = [profile.namespace, "__global__"]
//   - sync-back: same as shared, but writes back to parent
//   - ephemeral: not federated; worker queries only its own namespace
// Empty `namespaces` falls back to the default namespace so misuse
// at boundaries doesn't accidentally union the entire memory store.
func (s *Store) SearchInNamespaces(namespaces []string, queryVec []float32, topK int) ([]Memory, error) {
	if len(namespaces) == 0 {
		namespaces = []string{DefaultNamespace}
	}
	// Build IN-clause with placeholders.
	placeholders := make([]string, len(namespaces))
	args := make([]interface{}, len(namespaces))
	for i, ns := range namespaces {
		placeholders[i] = "?"
		args[i] = ns
	}
	q := `SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, namespace, created_at, embedding
		 FROM memories WHERE embedding IS NOT NULL AND namespace IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		Memory
		score float64
	}
	var candidates []scored
	for rows.Next() {
		var m Memory
		var embBlob []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
			&m.Role, &m.Wing, &m.Room, &m.Hall, &m.Namespace, &m.CreatedAt, &embBlob); err != nil {
			continue
		}
		if len(embBlob) == 0 {
			continue
		}
		vec := decodeVector(embBlob)
		sim := CosineSimilarity(queryVec, vec)
		s.decryptMemory(&m)
		candidates = append(candidates, scored{Memory: m, score: sim})
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].score > candidates[j].score })
	if topK > len(candidates) {
		topK = len(candidates)
	}
	results := make([]Memory, topK)
	for i := 0; i < topK; i++ {
		results[i] = candidates[i].Memory
		results[i].Similarity = candidates[i].score
	}
	return results, nil
}

// Search finds the top-K most similar memories for a project by cosine similarity.
func (s *Store) Search(projectDir string, queryVec []float32, topK int) ([]Memory, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at, embedding
		 FROM memories WHERE project_dir = ? AND embedding IS NOT NULL`,
		projectDir,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		Memory
		score float64
	}
	var candidates []scored

	for rows.Next() {
		var m Memory
		var embBlob []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
			&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt, &embBlob); err != nil {
			continue
		}
		if len(embBlob) == 0 {
			continue
		}
		vec := decodeVector(embBlob)
		sim := CosineSimilarity(queryVec, vec)
		s.decryptMemory(&m)
		candidates = append(candidates, scored{Memory: m, score: sim})
	}

	// Sort by similarity descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}
	results := make([]Memory, topK)
	for i := 0; i < topK; i++ {
		results[i] = candidates[i].Memory
		results[i].Similarity = candidates[i].score
	}
	return results, nil
}

// SearchAll searches across all projects.
func (s *Store) SearchAll(queryVec []float32, topK int) ([]Memory, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at, embedding
		 FROM memories WHERE embedding IS NOT NULL`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		Memory
		score float64
	}
	var candidates []scored

	for rows.Next() {
		var m Memory
		var embBlob []byte
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
			&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt, &embBlob); err != nil {
			continue
		}
		if len(embBlob) == 0 {
			continue
		}
		vec := decodeVector(embBlob)
		sim := CosineSimilarity(queryVec, vec)
		s.decryptMemory(&m)
		candidates = append(candidates, scored{Memory: m, score: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}
	results := make([]Memory, topK)
	for i := 0; i < topK; i++ {
		results[i] = candidates[i].Memory
		results[i].Similarity = candidates[i].score
	}
	return results, nil
}

// ListRecent returns the N most recent memories for a project.
func (s *Store) ListRecent(projectDir string, n int) ([]Memory, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at
		 FROM memories WHERE project_dir = ?
		 ORDER BY created_at DESC LIMIT ?`,
		projectDir, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
			&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt); err != nil {
			continue
		}
		s.decryptMemory(&m)
		result = append(result, m)
	}
	return result, nil
}

// ListByRole returns memories of a specific role for a project.
func (s *Store) ListByRole(projectDir, role string, n int) ([]Memory, error) {
	rows, err := s.db.Query(
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at
		 FROM memories WHERE project_dir = ? AND role = ?
		 ORDER BY created_at DESC LIMIT ?`,
		projectDir, role, n,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
			&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt); err != nil {
			continue
		}
		s.decryptMemory(&m)
		result = append(result, m)
	}
	return result, nil
}

// Delete removes a memory by ID.
func (s *Store) Delete(id int64) error {
	_, err := s.db.Exec(`DELETE FROM memories WHERE id = ?`, id)
	if err == nil {
		s.walLog("delete", map[string]interface{}{"id": id})
	}
	return err
}

// Count returns the total number of memories for a project.
func (s *Store) Count(projectDir string) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE project_dir = ?`, projectDir).Scan(&count)
	return count, err
}

// CountAll returns the total number of memories across all projects.
func (s *Store) CountAll() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&count)
	return count, err
}

// Prune removes memories older than the given duration.
func (s *Store) Prune(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(`DELETE FROM memories WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// PruneByRole deletes memories of a specific role older than the given duration.
// Returns the number of deleted rows.
func (s *Store) PruneByRole(role string, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := s.db.Exec(`DELETE FROM memories WHERE role = ? AND created_at < ?`, role, cutoff)
	if err != nil {
		return 0, err
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		s.walLog("prune", map[string]interface{}{"role": role, "older_than_days": int(olderThan.Hours() / 24), "deleted": n})
	}
	return n, nil
}

// RetentionPolicy defines per-role TTLs for automatic pruning.
type RetentionPolicy struct {
	SessionDays  int // 0 = keep forever
	ChunkDays    int
	ManualDays   int
	LearningDays int // typically 0 (keep forever)
}

// DefaultRetentionPolicy returns sensible defaults.
func DefaultRetentionPolicy() RetentionPolicy {
	return RetentionPolicy{
		SessionDays:  90,
		ChunkDays:    30,
		ManualDays:   0, // keep forever
		LearningDays: 0, // keep forever
	}
}

// ApplyRetention prunes memories according to the policy. Returns total deleted.
func (s *Store) ApplyRetention(policy RetentionPolicy) int64 {
	var total int64
	if policy.SessionDays > 0 {
		n, _ := s.PruneByRole("session", time.Duration(policy.SessionDays)*24*time.Hour)
		total += n
	}
	if policy.ChunkDays > 0 {
		n, _ := s.PruneByRole("output_chunk", time.Duration(policy.ChunkDays)*24*time.Hour)
		total += n
	}
	if policy.ManualDays > 0 {
		n, _ := s.PruneByRole("manual", time.Duration(policy.ManualDays)*24*time.Hour)
		total += n
	}
	if policy.LearningDays > 0 {
		n, _ := s.PruneByRole("learning", time.Duration(policy.LearningDays)*24*time.Hour)
		total += n
	}
	return total
}

// ListForReindex returns all memory IDs and content for batch re-embedding.
func (s *Store) ListForReindex() ([]struct{ ID int64; Content string }, error) {
	rows, err := s.db.Query(`SELECT id, content FROM memories ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []struct{ ID int64; Content string }
	for rows.Next() {
		var r struct{ ID int64; Content string }
		if rows.Scan(&r.ID, &r.Content) == nil {
			r.Content = s.decryptField(r.Content)
			result = append(result, r)
		}
	}
	return result, nil
}

// UpdateEmbedding sets the embedding vector for a memory by ID.
func (s *Store) UpdateEmbedding(id int64, embedding []float32) error {
	var embBlob []byte
	if len(embedding) > 0 {
		embBlob = encodeVector(embedding)
	}
	_, err := s.db.Exec(`UPDATE memories SET embedding = ? WHERE id = ?`, embBlob, id)
	return err
}

// SetScore sets a quality score on a memory (used for learning scoring BL53).
func (s *Store) SetScore(id int64, score int) error {
	// Score stored in summary field as "score:N" prefix for simplicity
	// (avoids schema migration for a single int)
	_, err := s.db.Exec(
		`UPDATE memories SET summary = 'score:' || ? || ' ' || COALESCE(NULLIF(summary,''),'') WHERE id = ?`,
		score, id,
	)
	return err
}

// MemoryStats holds aggregate memory system metrics.
type MemoryStats struct {
	TotalCount     int    `json:"total_count"`
	ManualCount    int    `json:"manual_count"`
	SessionCount   int    `json:"session_count"`
	LearningCount  int    `json:"learning_count"`
	ChunkCount     int    `json:"chunk_count"`
	DBSizeBytes    int64  `json:"db_size_bytes"`
	Encrypted      bool   `json:"encrypted"`
	KeyFingerprint string `json:"key_fingerprint,omitempty"`
}

// Stats returns aggregate metrics about the memory store.
func (s *Store) Stats() MemoryStats {
	var ms MemoryStats
	s.db.QueryRow(`SELECT COUNT(*) FROM memories`).Scan(&ms.TotalCount)                              //nolint:errcheck
	s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE role='manual'`).Scan(&ms.ManualCount)          //nolint:errcheck
	s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE role='session'`).Scan(&ms.SessionCount)        //nolint:errcheck
	s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE role='learning'`).Scan(&ms.LearningCount)      //nolint:errcheck
	s.db.QueryRow(`SELECT COUNT(*) FROM memories WHERE role='output_chunk'`).Scan(&ms.ChunkCount)     //nolint:errcheck
	// SQLite page_count * page_size gives DB file size
	s.db.QueryRow(`SELECT page_count * page_size FROM pragma_page_count(), pragma_page_size()`).Scan(&ms.DBSizeBytes) //nolint:errcheck
	ms.Encrypted = s.IsEncrypted()
	ms.KeyFingerprint = KeyFingerprint(s.encKey)
	return ms
}

// ListFiltered returns memories matching optional filters.
func (s *Store) ListFiltered(projectDir, role, since string, n int) ([]Memory, error) {
	query := `SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at FROM memories WHERE 1=1`
	var args []interface{}
	if projectDir != "" {
		query += ` AND project_dir = ?`
		args = append(args, projectDir)
	}
	if role != "" {
		query += ` AND role = ?`
		args = append(args, role)
	}
	if since != "" {
		query += ` AND created_at >= ?`
		args = append(args, since)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, n)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary,
			&m.Role, &m.Wing, &m.Room, &m.Hall, &m.CreatedAt); err != nil {
			continue
		}
		s.decryptMemory(&m)
		result = append(result, m)
	}
	return result, nil
}

// DistinctProjects returns all unique project directories that have memories.
func (s *Store) DistinctProjects() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT project_dir FROM memories ORDER BY project_dir`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var p string
		rows.Scan(&p) //nolint:errcheck
		result = append(result, p)
	}
	return result, nil
}

// ── Write-Ahead Log ──────────────────────────────────────────────────────────

// WALEntry represents a single write-ahead log entry.
type WALEntry struct {
	Timestamp string                 `json:"ts"`
	Operation string                 `json:"op"`
	Params    map[string]interface{} `json:"params"`
}

// walLog appends an entry to the write-ahead log file.
func (s *Store) walLog(operation string, params map[string]interface{}) {
	if s.walPath == "" {
		return
	}
	entry := WALEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Operation: operation,
		Params:    params,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	s.walMu.Lock()
	defer s.walMu.Unlock()
	f, err := os.OpenFile(s.walPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(data)       //nolint:errcheck
	f.Write([]byte("\n")) //nolint:errcheck
}

// WALRecent returns the last N entries from the write-ahead log.
func (s *Store) WALRecent(n int) []WALEntry {
	if s.walPath == "" {
		return nil
	}
	data, err := os.ReadFile(s.walPath)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	var entries []WALEntry
	for _, line := range lines {
		var e WALEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries
}

// ── Export / Import ──────────────────────────────────────────────────────────

// ExportMemory is the JSON representation for export (includes all fields).
// F10 S6.3 — Wing/Room/Hall + Namespace round-trip with import so
// sync-back preserves spatial structure + per-Project-Profile bucket.
type ExportMemory struct {
	ID         int64    `json:"id"`
	SessionID  string   `json:"session_id"`
	ProjectDir string   `json:"project_dir"`
	Content    string   `json:"content"`
	Summary    string   `json:"summary"`
	Role       string   `json:"role"`
	Wing       string   `json:"wing,omitempty"`
	Room       string   `json:"room,omitempty"`
	Hall       string   `json:"hall,omitempty"`
	Namespace  string   `json:"namespace,omitempty"`
	// Embedding is the JSON-serialised float32 vector. Round-tripped
	// through Export/Import so federated search hits imported rows
	// (S6.3 sync-back requires this — without the embedding the row
	// matches no `embedding IS NOT NULL` query).
	Embedding []float32 `json:"embedding,omitempty"`
	CreatedAt string   `json:"created_at"`
}

// Export writes all memories as a JSON array to the writer.
func (s *Store) Export(w io.Writer) error {
	rows, err := s.db.Query(`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, namespace, created_at FROM memories ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var memories []ExportMemory
	for rows.Next() {
		var m ExportMemory
		var t time.Time
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary, &m.Role, &m.Wing, &m.Room, &m.Hall, &m.Namespace, &t); err != nil {
			continue
		}
		m.CreatedAt = t.Format(time.RFC3339)
		memories = append(memories, m)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(memories)
}

// Import reads a JSON array of memories and inserts them. Honours
// per-row Wing/Room/Hall/Namespace so sync-back from a worker
// preserves spatial structure + the worker's namespace tag (F10 S6.3).
// Empty Namespace falls back to DefaultNamespace.
//
// Returns the number of imported memories.
func (s *Store) Import(r io.Reader) (int, error) {
	var memories []ExportMemory
	if err := json.NewDecoder(r).Decode(&memories); err != nil {
		return 0, fmt.Errorf("decode import: %w", err)
	}
	imported := 0
	for _, m := range memories {
		// Skip duplicates
		if dup := s.FindDuplicate(m.ProjectDir, m.Content); dup > 0 {
			continue
		}
		ns := m.Namespace
		if ns == "" {
			ns = DefaultNamespace
		}
		hash := contentHash(m.Content)
		var embBlob []byte
		if len(m.Embedding) > 0 {
			embBlob = encodeVector(m.Embedding)
		}
		_, err := s.db.Exec(
			`INSERT INTO memories (session_id, project_dir, content, summary, role, content_hash, wing, room, hall, namespace, embedding)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.SessionID, m.ProjectDir, m.Content, m.Summary, m.Role, hash,
			m.Wing, m.Room, m.Hall, ns, embBlob,
		)
		if err == nil {
			imported++
		}
	}
	s.walLog("import", map[string]interface{}{"count": imported})
	return imported, nil
}

// ExportSince writes all memories created strictly after `since` and
// (when namespace is non-empty) tagged with that namespace, as a JSON
// array. Used by F10 S6.3 sync-back: worker calls
// ExportSince(sessionStart, profile.namespace) on session-end and
// POSTs the body to parent's /api/memory/import.
//
// Implementation note: SQLite's CURRENT_TIMESTAMP uses text storage
// with second precision, and comparing time.Time values across that
// boundary is fiddly across driver versions. Safer to fetch all
// candidates by namespace and filter in Go — the volume here is
// bounded by a single session's writes (≪1000 rows), so the overhead
// is negligible.
func (s *Store) ExportSince(w io.Writer, namespace string, since time.Time) error {
	q := `SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, namespace, embedding, created_at
	      FROM memories`
	var args []interface{}
	if namespace != "" {
		q += ` WHERE namespace = ?`
		args = append(args, namespace)
	}
	q += ` ORDER BY id`

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	cutoff := since.UTC()
	var memories []ExportMemory
	for rows.Next() {
		var m ExportMemory
		var embBlob []byte
		var t time.Time
		if err := rows.Scan(&m.ID, &m.SessionID, &m.ProjectDir, &m.Content, &m.Summary, &m.Role, &m.Wing, &m.Room, &m.Hall, &m.Namespace, &embBlob, &t); err != nil {
			continue
		}
		if !t.UTC().After(cutoff) {
			continue
		}
		if len(embBlob) > 0 {
			m.Embedding = decodeVector(embBlob)
		}
		// decryptMemory mutates Content+Summary in place.
		mDecrypt := Memory{Content: m.Content, Summary: m.Summary}
		s.decryptMemory(&mDecrypt)
		m.Content = mDecrypt.Content
		m.Summary = mDecrypt.Summary
		m.CreatedAt = t.Format(time.RFC3339)
		memories = append(memories, m)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(memories)
}

// FindTunnels returns rooms that appear in multiple wings (cross-project links).
func (s *Store) FindTunnels() (map[string][]string, error) {
	rows, err := s.db.Query(
		`SELECT room, GROUP_CONCAT(DISTINCT wing) as wings
		 FROM memories WHERE room != '' AND wing != ''
		 GROUP BY room HAVING COUNT(DISTINCT wing) > 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tunnels := make(map[string][]string)
	for rows.Next() {
		var room, wingsStr string
		if rows.Scan(&room, &wingsStr) == nil {
			tunnels[room] = strings.Split(wingsStr, ",")
		}
	}
	return tunnels, nil
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// encodeVector serializes a float32 slice to bytes (little-endian).
func encodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// decodeVector deserializes bytes back to a float32 slice.
func decodeVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
