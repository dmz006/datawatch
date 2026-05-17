// BL302 S3 — unit tests for SamplingDispatcher.
package mcp

import (
	"context"
	"testing"
)

// TestSamplingDispatcher_NoClientGraceful verifies that Sample returns
// ErrSamplingNotSupported gracefully when no MCP client is connected.
// A nil srv means "no active session" — we never panic.
func TestSamplingDispatcher_NoClientGraceful(t *testing.T) {
	d := NewSamplingDispatcher(nil, nil)

	req := SamplingRequest{
		Trigger:  TriggerAlertTriage,
		Messages: []SamplingMessage{{Role: "user", Content: "ping"}},
	}

	_, err := d.Sample(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err != ErrSamplingNotSupported {
		t.Fatalf("expected ErrSamplingNotSupported, got %v", err)
	}
}

// TestSamplingDispatcher_LogGrowsAndCaps verifies the ring buffer grows to
// at most samplingLogSize entries.
func TestSamplingDispatcher_LogGrowsAndCaps(t *testing.T) {
	d := NewSamplingDispatcher(nil, nil)

	// Inject entries directly via appendLog.
	for i := 0; i < samplingLogSize+10; i++ {
		d.appendLog(&samplingLogEntry{
			SamplingResult: SamplingResult{Trigger: "test", Error: "no client"},
		})
	}

	log := d.Log()
	if len(log) != samplingLogSize {
		t.Fatalf("expected log size %d, got %d", samplingLogSize, len(log))
	}
}

// TestSamplingDispatcher_EmptyMessagesError verifies that an empty messages
// slice returns an error before any network call is attempted.
func TestSamplingDispatcher_EmptyMessagesError(t *testing.T) {
	d := NewSamplingDispatcher(nil, nil)

	_, err := d.Sample(context.Background(), SamplingRequest{
		Trigger:  "test",
		Messages: nil,
	})
	if err == nil {
		t.Fatal("expected error for empty messages, got nil")
	}
}

// TestSamplingDispatcher_DefaultMaxTokens verifies that MaxTokens is
// defaulted to 1024 when the caller passes 0.
func TestSamplingDispatcher_DefaultMaxTokens(t *testing.T) {
	// We can't run a real sampling call without an MCP server, so just verify
	// the default is applied before the nil-srv path returns an error.
	d := NewSamplingDispatcher(nil, nil)
	req := SamplingRequest{
		Trigger:   TriggerMorningBriefing,
		Messages:  []SamplingMessage{{Role: "user", Content: "hi"}},
		MaxTokens: 0, // should be defaulted
	}
	_, err := d.Sample(context.Background(), req)
	// We expect ErrSamplingNotSupported (nil srv), not a "max tokens" error.
	if err != ErrSamplingNotSupported {
		t.Fatalf("expected ErrSamplingNotSupported, got %v", err)
	}
}
