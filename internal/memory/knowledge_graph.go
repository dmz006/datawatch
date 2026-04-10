package memory

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// KGTriple represents an entity-relationship triple with temporal validity.
type KGTriple struct {
	ID          int64  `json:"id"`
	Subject     string `json:"subject"`
	Predicate   string `json:"predicate"`
	Object      string `json:"object"`
	ValidFrom   string `json:"valid_from,omitempty"`
	ValidTo     string `json:"valid_to,omitempty"`
	Source      string `json:"source,omitempty"` // session_id or "manual"
	CreatedAt   string `json:"created_at"`
}

// KGStats holds knowledge graph statistics.
type KGStats struct {
	EntityCount  int `json:"entity_count"`
	TripleCount  int `json:"triple_count"`
	ActiveCount  int `json:"active_count"` // triples without valid_to
	ExpiredCount int `json:"expired_count"`
}

// KnowledgeGraph provides temporal entity-relationship storage.
// Uses the same SQLite database as the memory store.
type KnowledgeGraph struct {
	db *sql.DB
}

// NewKnowledgeGraph creates a KG backed by the given store's database.
func NewKnowledgeGraph(store *Store) *KnowledgeGraph {
	kg := &KnowledgeGraph{db: store.db}
	kg.migrate()
	return kg
}

func (kg *KnowledgeGraph) migrate() {
	kg.db.Exec(`
		CREATE TABLE IF NOT EXISTS kg_entities (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			type TEXT DEFAULT 'unknown',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS kg_triples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			valid_from TEXT DEFAULT '',
			valid_to TEXT DEFAULT '',
			source TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_kg_subject ON kg_triples(subject);
		CREATE INDEX IF NOT EXISTS idx_kg_object ON kg_triples(object);
	`) //nolint:errcheck
}

// AddTriple adds an entity-relationship triple with optional temporal validity.
func (kg *KnowledgeGraph) AddTriple(subject, predicate, object, validFrom, source string) (int64, error) {
	// Ensure entities exist
	kg.ensureEntity(subject)
	kg.ensureEntity(object)

	if validFrom == "" {
		validFrom = time.Now().Format("2006-01-02")
	}

	result, err := kg.db.Exec(
		`INSERT INTO kg_triples (subject, predicate, object, valid_from, source) VALUES (?, ?, ?, ?, ?)`,
		subject, predicate, object, validFrom, source,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Invalidate marks a triple's validity as ended.
func (kg *KnowledgeGraph) Invalidate(subject, predicate, object, ended string) error {
	if ended == "" {
		ended = time.Now().Format("2006-01-02")
	}
	_, err := kg.db.Exec(
		`UPDATE kg_triples SET valid_to = ? WHERE subject = ? AND predicate = ? AND object = ? AND valid_to = ''`,
		ended, subject, predicate, object,
	)
	return err
}

// QueryEntity returns all triples involving an entity, optionally filtered by date.
func (kg *KnowledgeGraph) QueryEntity(name string, asOf string) ([]KGTriple, error) {
	query := `SELECT id, subject, predicate, object, valid_from, valid_to, source, created_at
		FROM kg_triples WHERE (subject = ? OR object = ?)`
	args := []interface{}{name, name}

	if asOf != "" {
		query += ` AND (valid_from <= ? OR valid_from = '') AND (valid_to >= ? OR valid_to = '')`
		args = append(args, asOf, asOf)
	}
	query += ` ORDER BY created_at DESC`

	return kg.queryTriples(query, args...)
}

// Timeline returns all triples for an entity in chronological order.
func (kg *KnowledgeGraph) Timeline(name string) ([]KGTriple, error) {
	return kg.queryTriples(
		`SELECT id, subject, predicate, object, valid_from, valid_to, source, created_at
		 FROM kg_triples WHERE subject = ? OR object = ?
		 ORDER BY valid_from ASC, created_at ASC`,
		name, name,
	)
}

// Stats returns KG statistics.
func (kg *KnowledgeGraph) Stats() KGStats {
	var s KGStats
	kg.db.QueryRow(`SELECT COUNT(DISTINCT name) FROM kg_entities`).Scan(&s.EntityCount)     //nolint:errcheck
	kg.db.QueryRow(`SELECT COUNT(*) FROM kg_triples`).Scan(&s.TripleCount)                   //nolint:errcheck
	kg.db.QueryRow(`SELECT COUNT(*) FROM kg_triples WHERE valid_to = ''`).Scan(&s.ActiveCount) //nolint:errcheck
	s.ExpiredCount = s.TripleCount - s.ActiveCount
	return s
}

// DeleteTriple removes a triple by ID.
func (kg *KnowledgeGraph) DeleteTriple(id int64) error {
	_, err := kg.db.Exec(`DELETE FROM kg_triples WHERE id = ?`, id)
	return err
}

func (kg *KnowledgeGraph) ensureEntity(name string) {
	kg.db.Exec(`INSERT OR IGNORE INTO kg_entities (name) VALUES (?)`, name) //nolint:errcheck
}

func (kg *KnowledgeGraph) queryTriples(query string, args ...interface{}) ([]KGTriple, error) {
	rows, err := kg.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []KGTriple
	for rows.Next() {
		var t KGTriple
		if err := rows.Scan(&t.ID, &t.Subject, &t.Predicate, &t.Object,
			&t.ValidFrom, &t.ValidTo, &t.Source, &t.CreatedAt); err != nil {
			continue
		}
		result = append(result, t)
	}
	return result, nil
}

// FormatTriples formats triples for display in comm channels.
func FormatTriples(triples []KGTriple) string {
	if len(triples) == 0 {
		return "  (none)"
	}
	var b strings.Builder
	for _, t := range triples {
		validity := ""
		if t.ValidFrom != "" {
			validity = fmt.Sprintf(" (from %s", t.ValidFrom)
			if t.ValidTo != "" {
				validity += fmt.Sprintf(" to %s", t.ValidTo)
			}
			validity += ")"
		}
		fmt.Fprintf(&b, "  #%d %s %s %s%s\n", t.ID, t.Subject, t.Predicate, t.Object, validity)
	}
	return b.String()
}
