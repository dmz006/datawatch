package memory

import (
	"strings"
	"testing"
)

func TestChunkText_ShortText(t *testing.T) {
	chunks := ChunkText("hello world", DefaultChunkConfig())
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != "hello world" {
		t.Errorf("chunk = %q, want 'hello world'", chunks[0])
	}
}

func TestChunkText_Empty(t *testing.T) {
	chunks := ChunkText("", DefaultChunkConfig())
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks for empty text, got %d", len(chunks))
	}
}

func TestChunkText_LongText(t *testing.T) {
	// Create text with 1000 words
	words := make([]string, 1000)
	for i := range words {
		words[i] = "word"
	}
	text := strings.Join(words, " ")

	cfg := ChunkConfig{MaxTokens: 300, OverlapTokens: 50}
	chunks := ChunkText(text, cfg)

	if len(chunks) < 3 {
		t.Errorf("expected at least 3 chunks for 1000 words at 300 max, got %d", len(chunks))
	}

	// Each chunk should have at most MaxTokens words
	for i, c := range chunks {
		wordCount := len(strings.Fields(c))
		if wordCount > cfg.MaxTokens {
			t.Errorf("chunk %d has %d words, max %d", i, wordCount, cfg.MaxTokens)
		}
	}
}

func TestChunkLines_Simple(t *testing.T) {
	lines := []string{
		"line one with some content",
		"line two with more content",
		"line three with extra content",
	}
	cfg := ChunkConfig{MaxTokens: 100, OverlapTokens: 0}
	chunks := ChunkLines(lines, cfg)
	if len(chunks) != 1 {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}
}

func TestChunkLines_Split(t *testing.T) {
	// Create many lines
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "this is a line with several words in it to make it longer")
	}
	cfg := ChunkConfig{MaxTokens: 50, OverlapTokens: 10}
	chunks := ChunkLines(lines, cfg)

	if len(chunks) < 2 {
		t.Errorf("expected multiple chunks, got %d", len(chunks))
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a, b     []float32
		expected float64
	}{
		{[]float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{[]float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{[]float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{[]float32{1, 1, 0}, []float32{1, 0, 0}, 0.7071},
		{[]float32{}, []float32{}, 0.0},
	}
	for _, tc := range tests {
		got := CosineSimilarity(tc.a, tc.b)
		if diff := got - tc.expected; diff > 0.001 || diff < -0.001 {
			t.Errorf("CosineSimilarity(%v, %v) = %f, want ~%f", tc.a, tc.b, got, tc.expected)
		}
	}
}
