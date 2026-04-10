// Package memory provides episodic memory with vector-indexed semantic search.
// It supports SQLite (pure Go, no cgo) and PostgreSQL+pgvector backends,
// with Ollama or OpenAI embedding providers.
package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder produces vector embeddings from text.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// OllamaEmbedder calls the Ollama /api/embed endpoint.
type OllamaEmbedder struct {
	host  string
	model string
	dims  int
}

// NewOllamaEmbedder creates an embedder using a local Ollama instance.
func NewOllamaEmbedder(host, model string, dims int) *OllamaEmbedder {
	if host == "" {
		host = "http://localhost:11434"
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	return &OllamaEmbedder{host: host, model: model, dims: dims}
}

func (e *OllamaEmbedder) Name() string    { return "ollama:" + e.model }
func (e *OllamaEmbedder) Dimensions() int { return e.dims }

func (e *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body := fmt.Sprintf(`{"model":%q,"input":%q}`, e.model, text)
	req, err := http.NewRequestWithContext(ctx, "POST", e.host+"/api/embed",
		strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed %d: %s", resp.StatusCode, string(b[:min(len(b), 200)]))
	}

	var result struct {
		Embeddings [][]float64 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode: %w", err)
	}
	if len(result.Embeddings) == 0 || len(result.Embeddings[0]) == 0 {
		return nil, fmt.Errorf("ollama returned empty embeddings")
	}

	// Convert float64 to float32
	vec := make([]float32, len(result.Embeddings[0]))
	for i, v := range result.Embeddings[0] {
		vec[i] = float32(v)
	}

	// Auto-detect dimensions on first call
	if e.dims == 0 {
		e.dims = len(vec)
	}

	return vec, nil
}

// OpenAIEmbedder calls the OpenAI embeddings API.
type OpenAIEmbedder struct {
	apiKey string
	model  string
	dims   int
}

// NewOpenAIEmbedder creates an embedder using OpenAI's API.
func NewOpenAIEmbedder(apiKey, model string, dims int) *OpenAIEmbedder {
	if model == "" {
		model = "text-embedding-3-small"
	}
	return &OpenAIEmbedder{apiKey: apiKey, model: model, dims: dims}
}

func (e *OpenAIEmbedder) Name() string    { return "openai:" + e.model }
func (e *OpenAIEmbedder) Dimensions() int { return e.dims }

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	body := fmt.Sprintf(`{"model":%q,"input":%q}`, e.model, text)
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings",
		strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai embed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("openai embed %d: %s", resp.StatusCode, string(b[:min(len(b), 200)]))
	}

	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("openai embed decode: %w", err)
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("openai returned empty embeddings")
	}

	vec := make([]float32, len(result.Data[0].Embedding))
	for i, v := range result.Data[0].Embedding {
		vec[i] = float32(v)
	}

	if e.dims == 0 {
		e.dims = len(vec)
	}

	return vec, nil
}

// CosineSimilarity computes the cosine similarity between two vectors.
// Returns a value between -1 and 1, where 1 means identical direction.
func CosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// TestEmbedder performs a full functional test of the embedding provider.
// Returns nil on success, or a descriptive error if any step fails.
// Tests: 1) connect to host, 2) send test embedding, 3) validate vector response.
func TestEmbedder(embedder Embedder) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	vec, err := embedder.Embed(ctx, "datawatch memory embedding test")
	if err != nil {
		return fmt.Errorf("embedding test failed: %w", err)
	}
	if len(vec) == 0 {
		return fmt.Errorf("embedding test failed: empty vector returned")
	}
	// Verify vector has non-zero values (not all zeros)
	hasNonZero := false
	for _, v := range vec {
		if v != 0 {
			hasNonZero = true
			break
		}
	}
	if !hasNonZero {
		return fmt.Errorf("embedding test failed: all-zero vector returned (model may not be loaded)")
	}
	return nil
}

// TestOllamaEmbedder creates a temporary Ollama embedder and runs the full test.
// Returns: dimensions on success, error on failure.
func TestOllamaEmbedder(host, model string) (int, error) {
	if host == "" {
		return 0, fmt.Errorf("ollama host not configured")
	}
	if model == "" {
		model = "nomic-embed-text"
	}
	embedder := NewOllamaEmbedder(host, model, 0)
	if err := TestEmbedder(embedder); err != nil {
		return 0, err
	}
	return embedder.Dimensions(), nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
