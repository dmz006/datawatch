package memory

import (
	"errors"
	"io"
	"time"
)

// ErrNamespaceUnsupported is returned when the active Backend doesn't
// implement NamespacedBackend (e.g. the Postgres path before pgvector
// SearchInNamespaces lands).
var ErrNamespaceUnsupported = errors.New("memory backend does not support namespace-filtered search")

// Backend is the interface that both SQLite Store and PGStore implement.
// The Retriever and adapters work against this interface.
type Backend interface {
	Save(projectDir, content, summary, role, sessionID string, embedding []float32) (int64, error)
	SaveWithMeta(projectDir, content, summary, role, sessionID, wing, room, hall string, embedding []float32) (int64, error)
	Search(projectDir string, queryVec []float32, topK int) ([]Memory, error)
	SearchAll(queryVec []float32, topK int) ([]Memory, error)
	SearchFiltered(wing, room string, queryVec []float32, topK int) ([]Memory, error)
	ListRecent(projectDir string, n int) ([]Memory, error)
	ListByRole(projectDir, role string, n int) ([]Memory, error)
	ListFiltered(projectDir, role, since string, n int) ([]Memory, error)
	Delete(id int64) error
	Count(projectDir string) (int, error)
	CountAll() (int, error)
	FindDuplicate(projectDir, content string) int64
	Prune(olderThan time.Duration) (int64, error)
	PruneByRole(role string, olderThan time.Duration) (int64, error)
	Stats() MemoryStats
	DistinctProjects() ([]string, error)
	ListWings() (map[string]int, error)
	ListRooms(wing string) ([]string, error)
	FindTunnels() (map[string][]string, error)
	ListForReindex() ([]struct{ ID int64; Content string }, error)
	UpdateEmbedding(id int64, embedding []float32) error
	ApplyRetention(policy RetentionPolicy) int64
	WALRecent(n int) []WALEntry
	SetScore(id int64, score int) error
	IsEncrypted() bool
	Close() error

	// Export/Import only on SQLite (PG uses pg_dump)
	Export(w io.Writer) error
	Import(r io.Reader) (int, error)
}

// NamespacedBackend is an optional capability extension a Backend
// implementation may also satisfy. SQLite Store implements it; the
// PG store path returns ErrNamespaceUnsupported when callers ask for
// namespace-filtered search until the matching pgvector query lands.
//
// BL101 uses this interface from the server-side cross-profile
// expansion path so callers don't have to type-assert against Store
// directly.
type NamespacedBackend interface {
	SearchInNamespaces(namespaces []string, queryVec []float32, topK int) ([]Memory, error)
}

// Compile-time interface checks
var _ Backend = (*Store)(nil)
var _ NamespacedBackend = (*Store)(nil)
