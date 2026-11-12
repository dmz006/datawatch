package transcribe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ChunkedTranscriber cuts a long recording into slices of at most Chunk and
// hands them to Inner one after another, so the engine never holds the whole
// clip in memory at once. The slice texts are trimmed and joined with a
// single space.
//
// Slicing never makes a transcription fail on its own: when the audio fits
// in one slice, or cannot be sliced (not a PCM WAV and no ffmpeg), the
// original file goes to Inner untouched.
type ChunkedTranscriber struct {
	Inner Transcriber
	// Chunk is the longest slice sent to Inner. Zero or negative turns
	// slicing off.
	Chunk time.Duration
	// Logf receives operator-visible progress lines. NewChunked defaults it
	// to fmt.Printf; a nil Logf discards them.
	Logf func(format string, args ...any)
}

// NewChunked wraps inner so recordings longer than chunk are sliced.
func NewChunked(inner Transcriber, chunk time.Duration) *ChunkedTranscriber {
	return &ChunkedTranscriber{Inner: inner, Chunk: chunk, Logf: func(format string, args ...any) {
		fmt.Printf(format, args...)
	}}
}

func (c *ChunkedTranscriber) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// Transcribe implements Transcriber.
func (c *ChunkedTranscriber) Transcribe(ctx context.Context, audioPath string) (string, error) {
	if c.Inner == nil {
		return "", errors.New("transcribe: chunked transcriber has no inner transcriber")
	}
	if c.Chunk <= 0 {
		return c.Inner.Transcribe(ctx, audioPath)
	}

	files, cleanup, err := splitAudioLong(ctx, audioPath, c.Chunk)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		c.logf("[voice] chunking skipped: %v; sending the whole file\n", err)
		return c.Inner.Transcribe(ctx, audioPath)
	}
	if len(files) == 0 {
		return c.Inner.Transcribe(ctx, audioPath)
	}

	n := len(files)
	c.logf("[voice] chunked: %d slices of <=%gs\n", n, c.Chunk.Seconds())

	parts := make([]string, 0, n)
	for i, f := range files {
		if err := ctx.Err(); err != nil {
			return "", fmt.Errorf("transcribe: stopped before slice %d of %d: %w", i+1, n, err)
		}
		text, err := c.Inner.Transcribe(ctx, f)
		if err != nil {
			return "", fmt.Errorf("transcribe: slice %d of %d failed: %w", i+1, n, err)
		}
		if text = strings.TrimSpace(text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " "), nil
}

// Preflight delegates to Inner when it supports a reachability check.
func (c *ChunkedTranscriber) Preflight(ctx context.Context) error {
	if pf, ok := c.Inner.(Preflighter); ok && pf != nil {
		return pf.Preflight(ctx)
	}
	return nil
}
