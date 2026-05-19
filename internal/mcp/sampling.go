// Package mcp — BL302 S3: daemon-initiated sampling infrastructure.
//
// SamplingDispatcher wraps the MCP SDK's RequestSampling call so any
// internal subsystem (alerts, Council, automata, morning briefing) can
// ask the connected Claude Code client for an LLM completion without
// coupling to the mcp-go server internals.
//
// When no MCP client is connected the dispatcher returns
// ErrSamplingNotSupported; callers degrade gracefully (e.g. fall back to
// the local LLM registry).
//
// A ring buffer of the last 50 results is kept in memory and exposed
// via datawatch://stats/mcp for debugging.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	mcpsdk "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/dmz006/datawatch/internal/stats"
)

// ErrSamplingNotSupported is returned when no MCP client with sampling
// capability is currently connected.  Callers should degrade gracefully.
var ErrSamplingNotSupported = errors.New("sampling not supported: no active MCP client with sampling capability")

// SamplingMessage mirrors mcpsdk.SamplingMessage for callers that don't
// import mcp-go directly.
type SamplingMessage struct {
	Role    string `json:"role"`    // "user" | "assistant"
	Content string `json:"content"` // plain text content
}

// SamplingRequest is the datawatch-layer request type for daemon-initiated sampling.
type SamplingRequest struct {
	// Trigger is a named trigger identifier (one of the TriggerXxx constants or a plugin event name).
	Trigger string `json:"trigger"`
	// Messages is the conversation history to send to the LLM.
	Messages []SamplingMessage `json:"messages"`
	// SystemPrompt is optional extra system context prepended to the request.
	SystemPrompt string `json:"system_prompt,omitempty"`
	// MaxTokens is the maximum number of tokens to generate (default 1024).
	MaxTokens int `json:"max_tokens,omitempty"`
}

// SamplingResult holds the result of a daemon-initiated sampling call.
type SamplingResult struct {
	// Trigger echoes the request trigger.
	Trigger string `json:"trigger"`
	// Content is the LLM-generated text.
	Content string `json:"content"`
	// Model is the model name reported by the client.
	Model string `json:"model,omitempty"`
	// StopReason is the stop reason reported by the client.
	StopReason string `json:"stop_reason,omitempty"`
	// LatencyMs is the round-trip latency in milliseconds.
	LatencyMs int64 `json:"latency_ms"`
	// Timestamp is when the result was received.
	Timestamp time.Time `json:"timestamp"`
	// Error is set when sampling failed (non-nil error path).
	Error string `json:"error,omitempty"`
}

// SamplingLogEntry is an entry in the ring buffer (includes request preview).
type SamplingLogEntry struct {
	SamplingResult
	RequestPreview string `json:"request_preview"` // first 80 chars of first user message
}

// samplingLogEntry is an alias kept for internal use.
type samplingLogEntry = SamplingLogEntry

const samplingLogSize = 50

// SamplingDispatcher dispatches daemon-initiated sampling requests to the
// connected MCP client (e.g. Claude Code).
type SamplingDispatcher struct {
	srv   *server.MCPServer
	stats *stats.MCPStatsCounters

	mu  sync.Mutex
	log []*samplingLogEntry // ring buffer, newest last
}

// NewSamplingDispatcher creates a SamplingDispatcher backed by srv.
// stats may be nil (no counters incremented).
func NewSamplingDispatcher(srv *server.MCPServer, sc *stats.MCPStatsCounters) *SamplingDispatcher {
	return &SamplingDispatcher{srv: srv, stats: sc}
}

// Sample sends a daemon-initiated sampling request to the connected MCP client.
// Returns ErrSamplingNotSupported when no client with sampling capability is
// connected; callers should degrade to a local LLM call in that case.
func (d *SamplingDispatcher) Sample(ctx context.Context, req SamplingRequest) (*SamplingResult, error) {
	if d.srv == nil {
		return nil, ErrSamplingNotSupported
	}

	if req.MaxTokens <= 0 {
		req.MaxTokens = 1024
	}

	// Build MCP SDK messages.
	sdkMsgs := make([]mcpsdk.SamplingMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := mcpsdk.RoleUser
		if m.Role == "assistant" {
			role = mcpsdk.RoleAssistant
		}
		sdkMsgs = append(sdkMsgs, mcpsdk.SamplingMessage{
			Role:    role,
			Content: mcpsdk.TextContent{Type: "text", Text: m.Content},
		})
	}
	if len(sdkMsgs) == 0 {
		return nil, fmt.Errorf("sampling request must have at least one message")
	}

	sdkReq := mcpsdk.CreateMessageRequest{
		Request: mcpsdk.Request{Method: string(mcpsdk.MethodSamplingCreateMessage)},
		CreateMessageParams: mcpsdk.CreateMessageParams{
			Messages:     sdkMsgs,
			SystemPrompt: req.SystemPrompt,
			MaxTokens:    req.MaxTokens,
		},
	}

	if d.stats != nil {
		d.stats.RecordSamplingReq()
	}

	start := time.Now()
	raw, err := d.srv.RequestSampling(ctx, sdkReq)
	latency := time.Since(start).Milliseconds()

	entry := &samplingLogEntry{
		SamplingResult: SamplingResult{
			Trigger:   req.Trigger,
			LatencyMs: latency,
			Timestamp: time.Now(),
		},
		RequestPreview: firstN(req.Messages[0].Content, 80),
	}

	if err != nil {
		// Map SDK errors to ErrSamplingNotSupported for "no active session"
		// variants so callers can test with errors.Is.
		if isNoSessionErr(err) {
			entry.Error = ErrSamplingNotSupported.Error()
			d.appendLog(entry)
			return nil, ErrSamplingNotSupported
		}
		entry.Error = err.Error()
		d.appendLog(entry)
		return nil, fmt.Errorf("sampling request failed: %w", err)
	}

	// Extract text content from result.
	content := ""
	if raw != nil {
		switch v := raw.Content.(type) {
		case mcpsdk.TextContent:
			content = v.Text
		case map[string]any:
			if t, ok := v["text"].(string); ok {
				content = t
			}
		default:
			content = fmt.Sprintf("%v", raw.Content)
		}
	}

	res := &SamplingResult{
		Trigger:    req.Trigger,
		Content:    content,
		LatencyMs:  latency,
		Timestamp:  time.Now(),
	}
	if raw != nil {
		res.Model = raw.Model
		res.StopReason = raw.StopReason
	}
	entry.SamplingResult = *res
	d.appendLog(entry)
	return res, nil
}

// Log returns a snapshot of the sampling ring buffer (newest last, max 50).
func (d *SamplingDispatcher) Log() []*SamplingLogEntry {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]*SamplingLogEntry, len(d.log))
	copy(out, d.log)
	return out
}

func (d *SamplingDispatcher) appendLog(e *samplingLogEntry) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.log = append(d.log, e)
	if len(d.log) > samplingLogSize {
		d.log = d.log[len(d.log)-samplingLogSize:]
	}
}

// isNoSessionErr reports whether err represents a "no active session" condition
// from the MCP SDK so we can map it to ErrSamplingNotSupported.
func isNoSessionErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return msg == "no active session" || msg == "session does not support sampling"
}

// firstN returns the first n runes of s, or all of s if shorter.
func firstN(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
