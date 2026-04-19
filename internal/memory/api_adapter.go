package memory

import "io"

// ServerAdapter implements server.MemoryAPI for the REST endpoints.
type ServerAdapter struct {
	retriever *Retriever
	defaultProject string
}

// NewServerAdapter creates an adapter for the HTTP server's memory API.
func NewServerAdapter(r *Retriever, defaultProject string) *ServerAdapter {
	return &ServerAdapter{retriever: r, defaultProject: defaultProject}
}

func (a *ServerAdapter) Stats() map[string]interface{} {
	ms := a.retriever.Store().Stats()
	return map[string]interface{}{
		"enabled":         true,
		"total_count":     ms.TotalCount,
		"manual_count":    ms.ManualCount,
		"session_count":   ms.SessionCount,
		"learning_count":  ms.LearningCount,
		"chunk_count":     ms.ChunkCount,
		"db_size_bytes":   ms.DBSizeBytes,
		"encrypted":       ms.Encrypted,
		"key_fingerprint": ms.KeyFingerprint,
	}
}

func (a *ServerAdapter) ListRecent(projectDir string, n int) ([]map[string]interface{}, error) {
	if projectDir == "" {
		projectDir = a.defaultProject
	}
	memories, err := a.retriever.Store().ListRecent(projectDir, n)
	if err != nil {
		return nil, err
	}
	return convertToMaps(memories), nil
}

func (a *ServerAdapter) Search(query string, topK int) ([]map[string]interface{}, error) {
	memories, err := a.retriever.RecallAll(query)
	if err != nil {
		return nil, err
	}
	return convertToMaps(memories), nil
}

// SearchInNamespaces (BL101) restricts the search to the supplied
// namespace set. Callers (the REST handler) pre-resolve the set via
// ProjectStore.EffectiveNamespacesFor so the worker only has to hand
// over its profile name.
func (a *ServerAdapter) SearchInNamespaces(query string, namespaces []string, topK int) ([]map[string]interface{}, error) {
	memories, err := a.retriever.RecallInNamespaces(query, namespaces)
	if err != nil {
		return nil, err
	}
	return convertToMaps(memories), nil
}

func (a *ServerAdapter) Delete(id int64) error {
	return a.retriever.Store().Delete(id)
}

func (a *ServerAdapter) Remember(projectDir, text string) (int64, error) {
	if projectDir == "" {
		projectDir = a.defaultProject
	}
	return a.retriever.Remember(projectDir, text)
}

func (a *ServerAdapter) ListFiltered(projectDir, role, since string, n int) ([]map[string]interface{}, error) {
	if projectDir == "" {
		projectDir = a.defaultProject
	}
	memories, err := a.retriever.Store().ListFiltered(projectDir, role, since, n)
	if err != nil {
		return nil, err
	}
	return convertToMaps(memories), nil
}

func (a *ServerAdapter) Export(w io.Writer) error {
	return a.retriever.Store().Export(w)
}

func (a *ServerAdapter) Import(r io.Reader) (int, error) {
	return a.retriever.Store().Import(r)
}

func (a *ServerAdapter) WALRecent(n int) []map[string]interface{} {
	entries := a.retriever.Store().WALRecent(n)
	result := make([]map[string]interface{}, len(entries))
	for i, e := range entries {
		result[i] = map[string]interface{}{
			"ts": e.Timestamp, "op": e.Operation, "params": e.Params,
		}
	}
	return result
}

func (a *ServerAdapter) Reindex() (int, error) {
	return a.retriever.Reindex()
}

func (a *ServerAdapter) ListLearnings(projectDir, query string, n int) ([]map[string]interface{}, error) {
	if projectDir == "" {
		projectDir = a.defaultProject
	}
	if query != "" {
		// Search learnings by query
		memories, err := a.retriever.Recall(projectDir, query)
		if err != nil {
			return nil, err
		}
		var learnings []Memory
		for _, m := range memories {
			if m.Role == "learning" {
				learnings = append(learnings, m)
			}
		}
		return convertToMaps(learnings), nil
	}
	// List recent learnings
	memories, err := a.retriever.Store().ListFiltered(projectDir, "learning", "", n)
	if err != nil {
		return nil, err
	}
	return convertToMaps(memories), nil
}

func (a *ServerAdapter) Research(query string, maxResults int) ([]map[string]interface{}, error) {
	if maxResults <= 0 {
		maxResults = 20
	}
	memories, err := a.retriever.RecallAll(query)
	if err != nil {
		return nil, err
	}
	if len(memories) > maxResults {
		memories = memories[:maxResults]
	}
	return convertToMaps(memories), nil
}

func convertToMaps(memories []Memory) []map[string]interface{} {
	result := make([]map[string]interface{}, len(memories))
	for i, m := range memories {
		result[i] = map[string]interface{}{
			"id":         m.ID,
			"session_id": m.SessionID,
			"project_dir": m.ProjectDir,
			"content":    m.Content,
			"summary":    m.Summary,
			"role":       m.Role,
			"wing":       m.Wing,
			"room":       m.Room,
			"hall":       m.Hall,
			"created_at": m.CreatedAt,
			"similarity": m.Similarity,
		}
	}
	return result
}
