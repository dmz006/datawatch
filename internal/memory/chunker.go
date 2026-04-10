package memory

import (
	"strings"
)

// ChunkConfig controls how session output is split for indexing.
type ChunkConfig struct {
	// MaxTokens is the approximate max tokens per chunk (default 500).
	MaxTokens int
	// OverlapTokens is the overlap between consecutive chunks (default 50).
	OverlapTokens int
}

// DefaultChunkConfig returns sensible defaults.
func DefaultChunkConfig() ChunkConfig {
	return ChunkConfig{MaxTokens: 500, OverlapTokens: 50}
}

// ChunkText splits text into overlapping chunks for granular vector indexing.
// Each chunk is approximately maxTokens words (using whitespace as a rough proxy
// for token boundaries — accurate enough for embedding search).
func ChunkText(text string, cfg ChunkConfig) []string {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 500
	}
	if cfg.OverlapTokens < 0 {
		cfg.OverlapTokens = 0
	}

	words := strings.Fields(text)
	if len(words) <= cfg.MaxTokens {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil
		}
		return []string{trimmed}
	}

	var chunks []string
	step := cfg.MaxTokens - cfg.OverlapTokens
	if step <= 0 {
		step = cfg.MaxTokens
	}

	for i := 0; i < len(words); i += step {
		end := i + cfg.MaxTokens
		if end > len(words) {
			end = len(words)
		}
		chunk := strings.Join(words[i:end], " ")
		if strings.TrimSpace(chunk) != "" {
			chunks = append(chunks, chunk)
		}
		if end == len(words) {
			break
		}
	}

	return chunks
}

// ChunkLines splits output lines into chunks, preserving line boundaries.
// This is better for session logs where line breaks are meaningful.
func ChunkLines(lines []string, cfg ChunkConfig) []string {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 500
	}

	var chunks []string
	var current []string
	currentWords := 0

	for _, line := range lines {
		lineWords := len(strings.Fields(line))
		if currentWords+lineWords > cfg.MaxTokens && len(current) > 0 {
			chunks = append(chunks, strings.Join(current, "\n"))
			// Overlap: keep last few lines
			overlapLines := 0
			overlapWords := 0
			for j := len(current) - 1; j >= 0 && overlapWords < cfg.OverlapTokens; j-- {
				overlapWords += len(strings.Fields(current[j]))
				overlapLines++
			}
			if overlapLines > 0 && overlapLines < len(current) {
				current = current[len(current)-overlapLines:]
				currentWords = overlapWords
			} else {
				current = nil
				currentWords = 0
			}
		}
		current = append(current, line)
		currentWords += lineWords
	}
	if len(current) > 0 {
		chunk := strings.Join(current, "\n")
		if strings.TrimSpace(chunk) != "" {
			chunks = append(chunks, chunk)
		}
	}

	return chunks
}
