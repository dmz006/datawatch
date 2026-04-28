package memory

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dmz006/datawatch/internal/router"
)

// RouterAdapter wraps a Retriever to implement router.MemoryRetriever.
type RouterAdapter struct {
	retriever *Retriever
}

// NewRouterAdapter creates an adapter for the router's memory interface.
func NewRouterAdapter(r *Retriever) *RouterAdapter {
	return &RouterAdapter{retriever: r}
}

func (a *RouterAdapter) Remember(projectDir, text string) (int64, error) {
	return a.retriever.Remember(projectDir, text)
}

func (a *RouterAdapter) Recall(projectDir, query string) ([]router.Memory, error) {
	results, err := a.retriever.Recall(projectDir, query)
	if err != nil {
		return nil, err
	}
	return convertMemories(results), nil
}

func (a *RouterAdapter) RecallAll(query string) ([]router.Memory, error) {
	results, err := a.retriever.RecallAll(query)
	if err != nil {
		return nil, err
	}
	return convertMemories(results), nil
}

func (a *RouterAdapter) Reindex() (int, error) {
	return a.retriever.Reindex()
}

func (a *RouterAdapter) Store() router.MemoryStore {
	return &storeAdapter{store: a.retriever.Store()}
}

type storeAdapter struct {
	store Backend
}

func (s *storeAdapter) ListRecent(projectDir string, n int) ([]router.Memory, error) {
	results, err := s.store.ListRecent(projectDir, n)
	if err != nil {
		return nil, err
	}
	return convertMemories(results), nil
}

func (s *storeAdapter) ListByRole(projectDir, role string, n int) ([]router.Memory, error) {
	results, err := s.store.ListByRole(projectDir, role, n)
	if err != nil {
		return nil, err
	}
	return convertMemories(results), nil
}

func (s *storeAdapter) Delete(id int64) error {
	return s.store.Delete(id)
}

func (s *storeAdapter) Count(projectDir string) (int, error) {
	return s.store.Count(projectDir)
}

func (s *storeAdapter) FindTunnels() (map[string][]string, error) {
	return s.store.FindTunnels()
}

func convertMemories(memories []Memory) []router.Memory {
	result := make([]router.Memory, len(memories))
	for i, m := range memories {
		result[i] = router.Memory{
			ID:         m.ID,
			SessionID:  m.SessionID,
			ProjectDir: m.ProjectDir,
			Content:    m.Content,
			Summary:    m.Summary,
			Role:       m.Role,
			Wing:       m.Wing,
			Room:       m.Room,
			Hall:       m.Hall,
			CreatedAt:  m.CreatedAt,
			Similarity: m.Similarity,
		}
	}
	return result
}

func (s *storeAdapter) Stats() router.MemoryStats {
	ms := s.store.Stats()
	return router.MemoryStats{
		TotalCount:     ms.TotalCount,
		ManualCount:    ms.ManualCount,
		SessionCount:   ms.SessionCount,
		LearningCount:  ms.LearningCount,
		ChunkCount:     ms.ChunkCount,
		DBSizeBytes:    ms.DBSizeBytes,
		Encrypted:      ms.Encrypted,
		KeyFingerprint: ms.KeyFingerprint,
	}
}

func (s *storeAdapter) Export(w io.Writer) error {
	return s.store.Export(w)
}

// SetPinned implements router.MemoryStore (v5.27.0 mempalace alignment).
// Falls back to ErrNamespaceUnsupported when the active backend doesn't
// support pinning (e.g. PG path before columns).
func (s *storeAdapter) SetPinned(id int64, pinned bool) error {
	pb, ok := s.store.(PinnableBackend)
	if !ok {
		return ErrNamespaceUnsupported
	}
	return pb.SetPinned(id, pinned)
}

// SweepStaleSummary returns a one-line chat-friendly description of
// the SweepStale result. (v5.27.0)
func (s *storeAdapter) SweepStaleSummary(olderThanDays int, dryRun bool) (string, error) {
	type sweeper interface {
		SweepStale(time.Duration, bool) (*SweepStaleResult, error)
	}
	sw, ok := s.store.(sweeper)
	if !ok {
		return "", ErrNamespaceUnsupported
	}
	res, err := sw.SweepStale(time.Duration(olderThanDays)*24*time.Hour, dryRun)
	if err != nil {
		return "", err
	}
	if res.DryRun {
		return fmt.Sprintf("memory sweep dry-run (>=%d days idle): %d candidates", olderThanDays, res.Candidates), nil
	}
	return fmt.Sprintf("memory sweep applied (>=%d days idle): %d candidates, %d deleted", olderThanDays, res.Candidates, res.Deleted), nil
}

// SpellCheckSummary returns a chat-friendly preview of suggestions.
func (s *storeAdapter) SpellCheckSummary(text string) string {
	suggestions := SpellCheck(text, SpellCheckOpts{})
	if len(suggestions) == 0 {
		return "spellcheck: no suggestions"
	}
	parts := make([]string, 0, len(suggestions))
	for _, sg := range suggestions {
		parts = append(parts, fmt.Sprintf("%s→%s", sg.Original, sg.Proposed))
	}
	return "spellcheck: " + strings.Join(parts, ", ")
}

// ExtractFactsSummary returns a chat-friendly preview of triples.
func (s *storeAdapter) ExtractFactsSummary(text string) string {
	triples, _ := ExtractFacts(context.Background(), text, nil)
	if len(triples) == 0 {
		return "extract_facts: no triples"
	}
	parts := make([]string, 0, len(triples))
	for _, t := range triples {
		parts = append(parts, fmt.Sprintf("(%s %s %s)", t.Subject, t.Predicate, t.Object))
	}
	return "extract_facts: " + strings.Join(parts, " ")
}

// SchemaVersionString returns the highest schema_version applied.
func (s *storeAdapter) SchemaVersionString() string {
	type versioned interface{ SchemaVersion() string }
	if v, ok := s.store.(versioned); ok {
		return v.SchemaVersion()
	}
	return ""
}
