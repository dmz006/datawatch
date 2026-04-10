package memory

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGStore is the PostgreSQL-backed memory store using pgvector for vector search.
type PGStore struct {
	pool       *pgxpool.Pool
	walPath    string
	walMu      sync.Mutex
	encKey     []byte
	hasVector  bool // true if pgvector extension is available
}

// NewPGStore opens a PostgreSQL memory store.
func NewPGStore(connURL string) (*PGStore, error) {
	pool, err := pgxpool.New(context.Background(), connURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	s := &PGStore{pool: pool}
	if err := s.migrate(); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate postgres: %w", err)
	}
	return s, nil
}

// NewPGStoreEncrypted opens an encrypted PostgreSQL store.
func NewPGStoreEncrypted(connURL string, encKey []byte) (*PGStore, error) {
	s, err := NewPGStore(connURL)
	if err != nil {
		return nil, err
	}
	if len(encKey) == 32 {
		s.encKey = make([]byte, 32)
		copy(s.encKey, encKey)
	}
	return s, nil
}

func (s *PGStore) migrate() error {
	ctx := context.Background()

	// Try to enable pgvector
	_, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS vector`)
	if err == nil {
		s.hasVector = true
	}

	// Create memories table
	_, err = s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS memories (
			id BIGSERIAL PRIMARY KEY,
			session_id TEXT DEFAULT '',
			project_dir TEXT NOT NULL,
			content TEXT NOT NULL,
			summary TEXT DEFAULT '',
			role TEXT DEFAULT 'session',
			wing TEXT DEFAULT '',
			room TEXT DEFAULT '',
			hall TEXT DEFAULT '',
			content_hash TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW(),
			embedding BYTEA
		)
	`)
	if err != nil {
		return err
	}

	// Create indices
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_pg_memories_project ON memories(project_dir)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_memories_role ON memories(role)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_memories_session ON memories(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_memories_hash ON memories(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_memories_wing ON memories(wing)`,
		`CREATE INDEX IF NOT EXISTS idx_pg_memories_room ON memories(room)`,
	} {
		s.pool.Exec(ctx, idx) //nolint:errcheck
	}

	// KG tables
	s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS kg_entities (
			id BIGSERIAL PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			type TEXT DEFAULT 'unknown',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`) //nolint:errcheck
	s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS kg_triples (
			id BIGSERIAL PRIMARY KEY,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			valid_from TEXT DEFAULT '',
			valid_to TEXT DEFAULT '',
			source TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)
	`) //nolint:errcheck
	s.pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_pg_kg_subject ON kg_triples(subject)`) //nolint:errcheck
	s.pool.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_pg_kg_object ON kg_triples(object)`)   //nolint:errcheck

	return nil
}

// IsEncrypted returns true if encryption is enabled.
func (s *PGStore) IsEncrypted() bool { return len(s.encKey) == 32 }

func (s *PGStore) encryptField(plaintext string) string {
	if len(s.encKey) != 32 || plaintext == "" {
		return plaintext
	}
	ct, err := fieldEncrypt([]byte(plaintext), s.encKey)
	if err != nil {
		return plaintext
	}
	return "ENC:" + ct
}

func (s *PGStore) decryptField(stored string) string {
	if len(s.encKey) != 32 || !strings.HasPrefix(stored, "ENC:") {
		return stored
	}
	pt, err := fieldDecrypt(stored[4:], s.encKey)
	if err != nil {
		return stored
	}
	return string(pt)
}

func (s *PGStore) decryptMemory(m *Memory) {
	m.Content = s.decryptField(m.Content)
	m.Summary = s.decryptField(m.Summary)
}

// Save stores a new memory. Deduplication via content_hash.
func (s *PGStore) Save(projectDir, content, summary, role, sessionID string, embedding []float32) (int64, error) {
	return s.SaveWithMeta(projectDir, content, summary, role, sessionID, "", "", "", embedding)
}

// SaveWithMeta stores a memory with spatial metadata.
func (s *PGStore) SaveWithMeta(projectDir, content, summary, role, sessionID, wing, room, hall string, embedding []float32) (int64, error) {
	hash := contentHash(content)

	// Dedup check
	var existingID int64
	err := s.pool.QueryRow(context.Background(),
		`SELECT id FROM memories WHERE project_dir = $1 AND content_hash = $2 LIMIT 1`,
		projectDir, hash).Scan(&existingID)
	if err == nil && existingID > 0 {
		return existingID, nil
	}

	if wing == "" {
		parts := strings.Split(projectDir, "/")
		if len(parts) > 0 {
			wing = parts[len(parts)-1]
		}
	}
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

	storedContent := s.encryptField(content)
	storedSummary := s.encryptField(summary)

	var embBlob []byte
	if len(embedding) > 0 {
		embBlob = pgEncodeVector(embedding)
	}

	var id int64
	err = s.pool.QueryRow(context.Background(),
		`INSERT INTO memories (session_id, project_dir, content, summary, role, embedding, content_hash, wing, room, hall)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		sessionID, projectDir, storedContent, storedSummary, role, embBlob, hash, wing, room, hall,
	).Scan(&id)
	if err != nil {
		return 0, err
	}

	s.walLog("save", map[string]interface{}{
		"id": id, "project_dir": projectDir, "role": role, "wing": wing, "room": room,
	})
	return id, nil
}

// Search finds top-K memories by cosine similarity.
func (s *PGStore) Search(projectDir string, queryVec []float32, topK int) ([]Memory, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at, embedding
		 FROM memories WHERE project_dir = $1 AND embedding IS NOT NULL`,
		projectDir)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAndRankByVec(rows, queryVec, topK)
}

// SearchAll searches across all projects.
func (s *PGStore) SearchAll(queryVec []float32, topK int) ([]Memory, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at, embedding
		 FROM memories WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAndRankByVec(rows, queryVec, topK)
}

// SearchFiltered searches with wing/room filtering.
func (s *PGStore) SearchFiltered(wing, room string, queryVec []float32, topK int) ([]Memory, error) {
	query := `SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at, embedding
		 FROM memories WHERE embedding IS NOT NULL`
	args := []interface{}{}
	argN := 1
	if wing != "" {
		query += fmt.Sprintf(` AND wing = $%d`, argN)
		args = append(args, wing)
		argN++
	}
	if room != "" {
		query += fmt.Sprintf(` AND room = $%d`, argN)
		args = append(args, room)
		argN++
	}
	rows, err := s.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanAndRankByVec(rows, queryVec, topK)
}

func (s *PGStore) scanAndRankByVec(rows interface{ Next() bool; Scan(...interface{}) error; Close() }, queryVec []float32, topK int) ([]Memory, error) {
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
		s.decryptMemory(&m)
		if len(embBlob) == 0 {
			continue
		}
		vec := pgDecodeVector(embBlob)
		sim := CosineSimilarity(queryVec, vec)
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

// ListRecent returns N most recent memories.
func (s *PGStore) ListRecent(projectDir string, n int) ([]Memory, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at
		 FROM memories WHERE project_dir = $1 ORDER BY created_at DESC LIMIT $2`,
		projectDir, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanMemories(rows)
}

// ListByRole returns memories of a specific role.
func (s *PGStore) ListByRole(projectDir, role string, n int) ([]Memory, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at
		 FROM memories WHERE project_dir = $1 AND role = $2 ORDER BY created_at DESC LIMIT $3`,
		projectDir, role, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanMemories(rows)
}

// ListFiltered returns memories with optional filters.
func (s *PGStore) ListFiltered(projectDir, role, since string, n int) ([]Memory, error) {
	query := `SELECT id, session_id, project_dir, content, summary, role, wing, room, hall, created_at FROM memories WHERE TRUE`
	args := []interface{}{}
	argN := 1
	if projectDir != "" {
		query += fmt.Sprintf(` AND project_dir = $%d`, argN)
		args = append(args, projectDir)
		argN++
	}
	if role != "" {
		query += fmt.Sprintf(` AND role = $%d`, argN)
		args = append(args, role)
		argN++
	}
	if since != "" {
		query += fmt.Sprintf(` AND created_at >= $%d`, argN)
		args = append(args, since)
		argN++
	}
	query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d`, argN)
	args = append(args, n)
	rows, err := s.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanMemories(rows)
}

func (s *PGStore) scanMemories(rows interface{ Next() bool; Scan(...interface{}) error }) ([]Memory, error) {
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
func (s *PGStore) Delete(id int64) error {
	_, err := s.pool.Exec(context.Background(), `DELETE FROM memories WHERE id = $1`, id)
	if err == nil {
		s.walLog("delete", map[string]interface{}{"id": id})
	}
	return err
}

// Count returns total memories for a project.
func (s *PGStore) Count(projectDir string) (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM memories WHERE project_dir = $1`, projectDir).Scan(&count)
	return count, err
}

// CountAll returns total memories.
func (s *PGStore) CountAll() (int, error) {
	var count int
	err := s.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM memories`).Scan(&count)
	return count, err
}

// FindDuplicate checks for existing content.
func (s *PGStore) FindDuplicate(projectDir, content string) int64 {
	hash := contentHash(content)
	var id int64
	s.pool.QueryRow(context.Background(),
		`SELECT id FROM memories WHERE project_dir = $1 AND content_hash = $2 LIMIT 1`,
		projectDir, hash).Scan(&id) //nolint:errcheck
	return id
}

// Prune deletes memories older than duration.
func (s *PGStore) Prune(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(context.Background(), `DELETE FROM memories WHERE created_at < $1`, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// PruneByRole prunes specific role.
func (s *PGStore) PruneByRole(role string, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tag, err := s.pool.Exec(context.Background(),
		`DELETE FROM memories WHERE role = $1 AND created_at < $2`, role, cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Stats returns aggregate metrics.
func (s *PGStore) Stats() MemoryStats {
	var ms MemoryStats
	ctx := context.Background()
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories`).Scan(&ms.TotalCount)                          //nolint:errcheck
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE role='manual'`).Scan(&ms.ManualCount)     //nolint:errcheck
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE role='session'`).Scan(&ms.SessionCount)   //nolint:errcheck
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE role='learning'`).Scan(&ms.LearningCount) //nolint:errcheck
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memories WHERE role='output_chunk'`).Scan(&ms.ChunkCount) //nolint:errcheck
	s.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&ms.DBSizeBytes)           //nolint:errcheck
	ms.Encrypted = s.IsEncrypted()
	ms.KeyFingerprint = KeyFingerprint(s.encKey)
	return ms
}

// DistinctProjects returns unique project directories.
func (s *PGStore) DistinctProjects() ([]string, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT DISTINCT project_dir FROM memories ORDER BY project_dir`)
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

// ListWings returns distinct wings with counts.
func (s *PGStore) ListWings() (map[string]int, error) {
	rows, err := s.pool.Query(context.Background(),
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

// ListRooms returns distinct rooms in a wing.
func (s *PGStore) ListRooms(wing string) ([]string, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT DISTINCT room FROM memories WHERE wing = $1 AND room != '' ORDER BY room`, wing)
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

// FindTunnels returns rooms shared across wings.
func (s *PGStore) FindTunnels() (map[string][]string, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT room, array_agg(DISTINCT wing) as wings
		 FROM memories WHERE room != '' AND wing != ''
		 GROUP BY room HAVING COUNT(DISTINCT wing) > 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tunnels := make(map[string][]string)
	for rows.Next() {
		var room string
		var wings []string
		if rows.Scan(&room, &wings) == nil {
			tunnels[room] = wings
		}
	}
	return tunnels, nil
}

// ListForReindex returns all IDs and content for batch re-embedding.
func (s *PGStore) ListForReindex() ([]struct{ ID int64; Content string }, error) {
	rows, err := s.pool.Query(context.Background(), `SELECT id, content FROM memories ORDER BY id`)
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

// UpdateEmbedding sets the embedding for a memory.
func (s *PGStore) UpdateEmbedding(id int64, embedding []float32) error {
	var embBlob []byte
	if len(embedding) > 0 {
		embBlob = pgEncodeVector(embedding)
	}
	_, err := s.pool.Exec(context.Background(), `UPDATE memories SET embedding = $1 WHERE id = $2`, embBlob, id)
	return err
}

// ApplyRetention applies per-role TTL pruning.
func (s *PGStore) ApplyRetention(policy RetentionPolicy) int64 {
	var total int64
	if policy.SessionDays > 0 {
		n, _ := s.PruneByRole("session", time.Duration(policy.SessionDays)*24*time.Hour)
		total += n
	}
	if policy.ChunkDays > 0 {
		n, _ := s.PruneByRole("output_chunk", time.Duration(policy.ChunkDays)*24*time.Hour)
		total += n
	}
	return total
}

// WALRecent returns recent WAL entries (uses same file-based WAL as SQLite).
func (s *PGStore) WALRecent(n int) []WALEntry {
	// WAL is file-based, not in postgres
	return nil
}

// SetScore sets a quality score on a memory.
func (s *PGStore) SetScore(id int64, score int) error {
	_, err := s.pool.Exec(context.Background(),
		`UPDATE memories SET summary = 'score:' || $1 || ' ' || COALESCE(NULLIF(summary,''),'') WHERE id = $2`,
		score, id)
	return err
}

// Export writes memories as JSON (stub — use pg_dump for PostgreSQL).
func (s *PGStore) Export(w io.Writer) error {
	memories, err := s.ListFiltered("", "", "", 10000)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(memories)
}

// Import reads JSON memories (stub).
func (s *PGStore) Import(r io.Reader) (int, error) {
	var memories []Memory
	if err := json.NewDecoder(r).Decode(&memories); err != nil {
		return 0, err
	}
	count := 0
	for _, m := range memories {
		if s.FindDuplicate(m.ProjectDir, m.Content) > 0 {
			continue
		}
		_, err := s.Save(m.ProjectDir, m.Content, m.Summary, m.Role, m.SessionID, nil)
		if err == nil {
			count++
		}
	}
	return count, nil
}

// Close closes the connection pool.
func (s *PGStore) Close() error {
	s.pool.Close()
	return nil
}

// walLog appends to the file-based WAL (shared with SQLite store).
func (s *PGStore) walLog(operation string, params map[string]interface{}) {
	// PG store doesn't use file-based WAL by default
	// Could be extended to use pg_notify or a WAL table
}

// ── Knowledge Graph (PG) ─────────────────────────────────────────────────────

// KG methods on PGStore — same API as KnowledgeGraph but using pgx.

// KGAddTriple adds a relationship triple.
func (s *PGStore) KGAddTriple(subject, predicate, object, validFrom, source string) (int64, error) {
	ctx := context.Background()
	s.pool.Exec(ctx, `INSERT INTO kg_entities (name) VALUES ($1) ON CONFLICT DO NOTHING`, subject) //nolint:errcheck
	s.pool.Exec(ctx, `INSERT INTO kg_entities (name) VALUES ($1) ON CONFLICT DO NOTHING`, object)  //nolint:errcheck
	if validFrom == "" {
		validFrom = time.Now().Format("2006-01-02")
	}
	var id int64
	err := s.pool.QueryRow(ctx,
		`INSERT INTO kg_triples (subject, predicate, object, valid_from, source) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		subject, predicate, object, validFrom, source).Scan(&id)
	return id, err
}

// KGInvalidate ends a triple's validity.
func (s *PGStore) KGInvalidate(subject, predicate, object, ended string) error {
	if ended == "" {
		ended = time.Now().Format("2006-01-02")
	}
	_, err := s.pool.Exec(context.Background(),
		`UPDATE kg_triples SET valid_to = $1 WHERE subject = $2 AND predicate = $3 AND object = $4 AND valid_to = ''`,
		ended, subject, predicate, object)
	return err
}

// KGQueryEntity returns triples involving an entity.
func (s *PGStore) KGQueryEntity(name, asOf string) ([]KGTriple, error) {
	query := `SELECT id, subject, predicate, object, valid_from, valid_to, source, created_at
		FROM kg_triples WHERE (subject = $1 OR object = $1)`
	args := []interface{}{name}
	if asOf != "" {
		query += ` AND (valid_from <= $2 OR valid_from = '') AND (valid_to >= $2 OR valid_to = '')`
		args = append(args, asOf)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []KGTriple
	for rows.Next() {
		var t KGTriple
		rows.Scan(&t.ID, &t.Subject, &t.Predicate, &t.Object, &t.ValidFrom, &t.ValidTo, &t.Source, &t.CreatedAt) //nolint:errcheck
		result = append(result, t)
	}
	return result, nil
}

// KGTimeline returns chronological triples for an entity.
func (s *PGStore) KGTimeline(name string) ([]KGTriple, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, subject, predicate, object, valid_from, valid_to, source, created_at
		 FROM kg_triples WHERE subject = $1 OR object = $1
		 ORDER BY valid_from ASC, created_at ASC`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []KGTriple
	for rows.Next() {
		var t KGTriple
		rows.Scan(&t.ID, &t.Subject, &t.Predicate, &t.Object, &t.ValidFrom, &t.ValidTo, &t.Source, &t.CreatedAt) //nolint:errcheck
		result = append(result, t)
	}
	return result, nil
}

// KGStats returns KG statistics.
func (s *PGStore) KGStats() KGStats {
	var st KGStats
	ctx := context.Background()
	s.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT name) FROM kg_entities`).Scan(&st.EntityCount)      //nolint:errcheck
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM kg_triples`).Scan(&st.TripleCount)                    //nolint:errcheck
	s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM kg_triples WHERE valid_to = ''`).Scan(&st.ActiveCount) //nolint:errcheck
	st.ExpiredCount = st.TripleCount - st.ActiveCount
	return st
}

// pgEncodeVector serializes float32 slice to bytes for storage.
func pgEncodeVector(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// pgDecodeVector deserializes bytes to float32 slice.
func pgDecodeVector(b []byte) []float32 {
	n := len(b) / 4
	v := make([]float32, n)
	for i := 0; i < n; i++ {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}
