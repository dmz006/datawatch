package memory

import (
	"context"
	"io"
	"time"
)

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

// SetPinned toggles the pinned flag (Mempalace QW#2, v5.26.70). Returns
// ErrNamespaceUnsupported when the active backend doesn't implement
// PinnableBackend (e.g. PG path until the column is added).
func (a *ServerAdapter) SetPinned(id int64, pinned bool) error {
	pb, ok := a.retriever.Store().(PinnableBackend)
	if !ok {
		return ErrNamespaceUnsupported
	}
	return pb.SetPinned(id, pinned)
}

// SweepStale (v6.0.0) drops rows whose last_hit_at is zero AND
// created_at is older than the cutoff. Returns the candidate +
// deleted counts. Manual + pinned rows are exempt.
//
// Backends without the SweepStale capability (e.g. the PG path
// before the columns land) return ErrNamespaceUnsupported.
func (a *ServerAdapter) SweepStale(olderThanDays int, dryRun bool) (map[string]interface{}, error) {
	d := time.Duration(olderThanDays) * 24 * time.Hour
	type sweeper interface {
		SweepStale(time.Duration, bool) (*SweepStaleResult, error)
	}
	sw, ok := a.retriever.Store().(sweeper)
	if !ok {
		return nil, ErrNamespaceUnsupported
	}
	res, err := sw.SweepStale(d, dryRun)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"candidates": res.Candidates,
		"deleted":    res.Deleted,
		"dry_run":    res.DryRun,
	}, nil
}

// SpellCheckText (v6.0.0) returns suggestions without rewriting.
func (a *ServerAdapter) SpellCheckText(text string, extra []string) []map[string]interface{} {
	suggestions := SpellCheck(text, SpellCheckOpts{ExtraWords: extra})
	out := make([]map[string]interface{}, len(suggestions))
	for i, s := range suggestions {
		out[i] = map[string]interface{}{
			"original": s.Original,
			"proposed": s.Proposed,
			"distance": s.Distance,
		}
	}
	return out
}

// ExtractFactsText (v6.0.0) runs the heuristic schema-free triple
// extractor. LLM fallback is left out of the REST path to keep it
// dependency-free; programmatic callers can use the package-level
// ExtractFacts directly.
func (a *ServerAdapter) ExtractFactsText(text string) []map[string]interface{} {
	triples, _ := ExtractFacts(context.Background(), text, nil)
	out := make([]map[string]interface{}, len(triples))
	for i, t := range triples {
		out[i] = map[string]interface{}{
			"subject":    t.Subject,
			"predicate":  t.Predicate,
			"object":     t.Object,
			"confidence": t.Confidence,
			"source":     t.Source,
		}
	}
	return out
}

// SchemaVersion (v6.0.0) returns the highest schema_version row
// the SQLite migrate path applied. Empty string when the active
// backend doesn't expose a SchemaVersion method (e.g. PG path).
func (a *ServerAdapter) SchemaVersion() string {
	type versioned interface{ SchemaVersion() string }
	if v, ok := a.retriever.Store().(versioned); ok {
		return v.SchemaVersion()
	}
	return ""
}

// WakeUpBundle (v5.26.71) returns the composed L0+L1 wake-up
// context. When agentID is non-empty, the L4/L5 recursive layers
// are stitched in too — same composition the agent bootstrap path
// produces. Used by /api/memory/wakeup so smoke + operator tooling
// can verify what an agent would actually see at start.
func (a *ServerAdapter) WakeUpBundle(projectDir, selfAgentID, parentAgentID, parentNamespace string) string {
	if projectDir == "" {
		projectDir = a.defaultProject
	}
	layers := NewLayers(projectDir, a.retriever)
	if selfAgentID != "" || parentAgentID != "" {
		return layers.WakeUpContextForAgent(selfAgentID, parentAgentID, parentNamespace, projectDir)
	}
	return layers.WakeUpContext(projectDir)
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
			"floor":      m.Floor,
			"shelf":      m.Shelf,
			"box":        m.Box,
			"source":     m.Source,
			"last_hit_at": m.LastHitAt,
			"pinned":     m.Pinned,
			"created_at": m.CreatedAt,
			"similarity": m.Similarity,
		}
	}
	return result
}
