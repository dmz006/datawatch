package memory

import (
	"io"

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
