package memory

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// Retriever orchestrates memory storage and retrieval with embeddings.
type Retriever struct {
	store    Backend
	embedder Embedder
	topK     int
}

// NewRetriever creates a retriever with the given store and embedder.
func NewRetriever(store Backend, embedder Embedder, topK int) *Retriever {
	if topK <= 0 {
		topK = 5
	}
	return &Retriever{store: store, embedder: embedder, topK: topK}
}

// Store returns the underlying backend.
func (r *Retriever) Store() Backend { return r.store }

// Embedder returns the configured embedder. Used by callers that want
// to share the operator's configured Ollama / OpenAI without duplicating
// the wiring. BL274 v6.17.0 — docsindex vector layer reuses this.
func (r *Retriever) Embedder() Embedder { return r.embedder }

// Remember saves a manual memory with embedding.
func (r *Retriever) Remember(projectDir, text string) (int64, error) {
	if r.embedder == nil {
		// No embedder — save without vector
		return r.store.Save(projectDir, text, "", "manual", "", nil)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vec, err := r.embedder.Embed(ctx, text)
	if err != nil {
		// Save without embedding — still searchable by listing
		log.Printf("[memory] embedding failed, saving without vector: %v", err)
		return r.store.Save(projectDir, text, "", "manual", "", nil)
	}
	return r.store.Save(projectDir, text, "", "manual", "", vec)
}

// textFallback returns memories via full-text keyword search when the embedding
// LLM is unavailable. It logs the original embed error once at warn level.
// Results have Similarity=0 to signal keyword-match rather than semantic rank.
func (r *Retriever) textFallback(projectDir, query string, embedErr error) ([]Memory, error) {
	log.Printf("[memory] embed unavailable (%v), falling back to keyword search", embedErr)
	tsb, ok := r.store.(TextSearchableBackend)
	if !ok {
		return nil, fmt.Errorf("embed query: %w (no text-search fallback)", embedErr)
	}
	if projectDir != "" {
		return tsb.SearchByText(projectDir, query, r.topK)
	}
	return tsb.SearchAllByText(query, r.topK)
}

// Recall performs semantic search across memories for a project.
// Query is sanitized for prompt-injection patterns (v5.26.70 — QW#4)
// before reaching the embedder.
// When the embedder is unavailable, falls back to keyword search (Similarity=0).
func (r *Retriever) Recall(projectDir, query string) ([]Memory, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if r.embedder == nil {
		return r.textFallback(projectDir, query, fmt.Errorf("no embedder configured"))
	}
	cleaned, _ := SanitizeQuery(query)
	vec, err := r.embedder.Embed(ctx, cleaned)
	if err != nil {
		return r.textFallback(projectDir, cleaned, err)
	}
	return r.store.Search(projectDir, vec, r.topK)
}

// RecallAll performs semantic search across all projects.
// Falls back to keyword search when the embedder is unavailable.
func (r *Retriever) RecallAll(query string) ([]Memory, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if r.embedder == nil {
		return r.textFallback("", query, fmt.Errorf("no embedder configured"))
	}
	cleaned, _ := SanitizeQuery(query)
	vec, err := r.embedder.Embed(ctx, cleaned)
	if err != nil {
		return r.textFallback("", cleaned, err)
	}
	return r.store.SearchAll(vec, r.topK)
}

// RecallInNamespaces (BL101) performs semantic search restricted to
// the supplied namespace set. The set is pre-resolved by the caller —
// usually from ProjectStore.EffectiveNamespacesFor(profileName), so a
// worker can pass its profile name and the parent expands to the
// mutual-opt-in union without the worker having to know peer
// namespaces.
//
// Returns ErrNamespaceUnsupported when the configured Backend isn't a
// NamespacedBackend (e.g. PG path until pgvector lands).
// Falls back to keyword search within the namespaces when the embedder
// is unavailable.
func (r *Retriever) RecallInNamespaces(query string, namespaces []string) ([]Memory, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	nb, ok := r.store.(NamespacedBackend)
	cleaned, _ := SanitizeQuery(query)

	if r.embedder == nil {
		if tsb, ok2 := r.store.(TextSearchableBackend); ok2 {
			log.Printf("[memory] embed unavailable (no embedder), falling back to keyword namespace search")
			return tsb.SearchInNamespacesByText(namespaces, cleaned, r.topK)
		}
		if !ok {
			return nil, ErrNamespaceUnsupported
		}
		return nil, fmt.Errorf("embed query: no embedder configured (no text-search fallback)")
	}
	if !ok {
		return nil, ErrNamespaceUnsupported
	}
	vec, err := r.embedder.Embed(ctx, cleaned)
	if err != nil {
		if tsb, ok2 := r.store.(TextSearchableBackend); ok2 {
			log.Printf("[memory] embed unavailable (%v), falling back to keyword namespace search", err)
			return tsb.SearchInNamespacesByText(namespaces, cleaned, r.topK)
		}
		return nil, fmt.Errorf("embed query: %w", err)
	}
	return nb.SearchInNamespaces(namespaces, vec, r.topK)
}

// SaveSessionSummary stores a session summary with embedding on completion.
func (r *Retriever) SaveSessionSummary(projectDir, sessionID, task, summary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	content := fmt.Sprintf("Task: %s\nSummary: %s", task, summary)
	vec, err := r.embedder.Embed(ctx, content)
	if err != nil {
		log.Printf("[memory] embedding session summary failed: %v", err)
		vec = nil
	}
	_, err = r.store.Save(projectDir, content, summary, "session", sessionID, vec)
	return err
}

// SaveLearning stores a task learning with embedding.
func (r *Retriever) SaveLearning(projectDir, sessionID, learning string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	vec, err := r.embedder.Embed(ctx, learning)
	if err != nil {
		log.Printf("[memory] embedding learning failed: %v", err)
		vec = nil
	}
	_, err = r.store.Save(projectDir, learning, "", "learning", sessionID, vec)
	return err
}

// SaveOutputChunks stores chunked session output for granular search.
// When the embedder is unavailable, chunks are saved without a vector
// so they remain full-text searchable and can be re-embedded later via
// LazyReembed when the LLM comes back online.
func (r *Retriever) SaveOutputChunks(projectDir, sessionID string, chunks []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for _, chunk := range chunks {
		var vec []float32
		if r.embedder != nil {
			var err error
			vec, err = r.embedder.Embed(ctx, chunk)
			if err != nil {
				log.Printf("[memory] embedding chunk failed, saving without vector: %v", err)
				vec = nil
			}
		}
		if _, err := r.store.Save(projectDir, chunk, "", "output_chunk", sessionID, vec); err != nil {
			log.Printf("[memory] save chunk: %v", err)
		}
	}
	return nil
}

// LazyReembed attempts to back-fill embedding vectors for memories stored
// without one (e.g. when Ollama was offline during Save). It processes up
// to batchSize memories per call and returns the number successfully embedded.
// Callers should invoke this periodically after detecting that the LLM has
// become available; it is safe to call concurrently with normal recall/save.
func (r *Retriever) LazyReembed(batchSize int) (int, error) {
	if r.embedder == nil {
		return 0, nil
	}
	tsb, ok := r.store.(TextSearchableBackend)
	if !ok {
		return 0, nil
	}
	items, err := tsb.ListUnembedded(batchSize)
	if err != nil || len(items) == 0 {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	count := 0
	for _, m := range items {
		if m.Content == "" {
			continue
		}
		vec, err := r.embedder.Embed(ctx, m.Content)
		if err != nil {
			log.Printf("[memory] lazy-reembed skip #%d: %v", m.ID, err)
			break // embedder down — stop and retry next time
		}
		if err := r.store.UpdateEmbedding(m.ID, vec); err != nil {
			log.Printf("[memory] lazy-reembed update #%d: %v", m.ID, err)
			continue
		}
		count++
	}
	if count > 0 {
		log.Printf("[memory] lazy-reembed: embedded %d/%d pending memories", count, len(items))
	}
	return count, nil
}

// FormatRecallResults formats search results for display in comm channels.
func FormatRecallResults(memories []Memory) string {
	if len(memories) == 0 {
		return "No matching memories found."
	}
	var b strings.Builder
	for i, m := range memories {
		sim := fmt.Sprintf("%.0f%%", m.Similarity*100)
		date := m.CreatedAt.Format("2006-01-02")
		role := m.Role
		content := m.Content
		if len(content) > 150 {
			content = content[:147] + "..."
		}
		// Replace newlines with spaces for compact display
		content = strings.ReplaceAll(content, "\n", " ")
		if m.SessionID != "" {
			fmt.Fprintf(&b, "%d. [%s] %s (%s, %s) #%d\n   %s\n", i+1, sim, role, date, m.SessionID, m.ID, content)
		} else {
			fmt.Fprintf(&b, "%d. [%s] %s (%s) #%d\n   %s\n", i+1, sim, role, date, m.ID, content)
		}
	}
	return b.String()
}

// FormatMemoryList formats a list of memories for display.
func FormatMemoryList(memories []Memory) string {
	if len(memories) == 0 {
		return "No memories stored."
	}
	var b strings.Builder
	for _, m := range memories {
		date := m.CreatedAt.Format("01-02 15:04")
		content := m.Content
		if len(content) > 100 {
			content = content[:97] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Fprintf(&b, "#%d [%s] %s: %s\n", m.ID, date, m.Role, content)
	}
	return b.String()
}

// RetrieveContext searches memory for relevant context based on a task description
// and returns a formatted context string suitable for injecting into a session.
// Returns empty string if no relevant memories found or on error.
// When the embedder is unavailable, falls back to keyword search with no
// similarity threshold (all keyword matches are included).
func (r *Retriever) RetrieveContext(projectDir, task string, topK int) string {
	if task == "" || topK <= 0 {
		return ""
	}

	var memories []Memory

	if r.embedder != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		vec, err := r.embedder.Embed(ctx, task)
		cancel()
		if err != nil {
			log.Printf("[memory] auto-retrieve embed failed: %v; trying keyword fallback", err)
		} else {
			memories, err = r.store.Search(projectDir, vec, topK)
			if err != nil {
				log.Printf("[memory] auto-retrieve search failed: %v", err)
				memories = nil
			}
		}
	}

	// Keyword fallback when embed unavailable or returned nothing.
	if len(memories) == 0 {
		if tsb, ok := r.store.(TextSearchableBackend); ok {
			var ferr error
			memories, ferr = tsb.SearchByText(projectDir, task, topK)
			if ferr != nil {
				log.Printf("[memory] keyword fallback search failed: %v", ferr)
				return ""
			}
		} else {
			return ""
		}
	}

	if len(memories) == 0 {
		return ""
	}

	// For semantic results filter by similarity; for keyword results include all.
	var relevant []Memory
	for _, m := range memories {
		if m.Similarity > 0.3 || m.Similarity == 0 {
			relevant = append(relevant, m)
		}
	}
	if len(relevant) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Relevant context from memory\n\n")
	for i, m := range relevant {
		content := m.Content
		if len(content) > 300 {
			content = content[:297] + "..."
		}
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, m.Role, content)
	}
	return b.String()
}

// Reindex re-embeds all memories with the current embedder. Used after model change.
// Returns the number of re-embedded memories.
func (r *Retriever) Reindex() (int, error) {
	items, err := r.store.ListForReindex()
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	count := 0
	for _, item := range items {
		if item.Content == "" {
			continue
		}
		vec, err := r.embedder.Embed(ctx, item.Content)
		if err != nil {
			log.Printf("[memory] reindex skip #%d: %v", item.ID, err)
			continue
		}
		if err := r.store.UpdateEmbedding(item.ID, vec); err != nil {
			log.Printf("[memory] reindex update #%d: %v", item.ID, err)
			continue
		}
		count++
	}
	log.Printf("[memory] reindex complete: %d/%d memories re-embedded", count, len(items))
	return count, nil
}

// Close closes the retriever's store.
func (r *Retriever) Close() error {
	return r.store.Close()
}
