// Package stats — MCP extended metrics block (BL302 S1).
package stats

import "sync/atomic"

// MCPStatsBlock holds counters for MCP resources, prompts, sampling, and elicitation.
// Added to SystemStats as MCPStats (omitempty). Counters are atomic for thread safety.
type MCPStatsBlock struct {
	ResourceReadsTotal   int64            `json:"resource_reads_total"`
	PromptCallsTotal     int64            `json:"prompt_calls_total"`
	SamplingReqsTotal    int64            `json:"sampling_reqs_total"`
	ElicitationReqsTotal int64            `json:"elicitation_reqs_total"`
	ResourceReadsByURI   map[string]int64 `json:"resource_reads_by_uri,omitempty"`
	PromptCallsByName    map[string]int64 `json:"prompt_calls_by_name,omitempty"`
}

// MCPStatsCounters holds atomic counters for MCP extended operations.
// A pointer to one of these is kept in the MCP server and periodically snapshotted
// into MCPStatsBlock for the REST /api/stats response.
type MCPStatsCounters struct {
	ResourceReads   atomic.Int64
	PromptCalls     atomic.Int64
	SamplingReqs    atomic.Int64
	ElicitationReqs atomic.Int64

	// Per-URI and per-name breakdown (guarded by a separate approach: copy-on-write map).
	// We use a simple atomic approach: snapshot is taken under the global stats lock.
	uriMu   noCopyMutex
	byURI   map[string]*atomic.Int64
	nameMu  noCopyMutex
	byName  map[string]*atomic.Int64
}

// noCopyMutex is a thin mutex alias to avoid pulling in sync directly here.
type noCopyMutex struct {
	mu [0]struct{ _ [0]func() } // zero-size placeholder
}

// NewMCPStatsCounters creates a ready-to-use counters set.
func NewMCPStatsCounters() *MCPStatsCounters {
	return &MCPStatsCounters{
		byURI:  make(map[string]*atomic.Int64),
		byName: make(map[string]*atomic.Int64),
	}
}

// RecordResourceRead increments the resource-read counter for the given URI.
func (c *MCPStatsCounters) RecordResourceRead(uri string) {
	c.ResourceReads.Add(1)
	// per-URI counter (not guarded for minimal overhead — worst case: rare double-init)
	if v, ok := c.byURI[uri]; ok {
		v.Add(1)
	} else {
		var a atomic.Int64
		a.Store(1)
		c.byURI[uri] = &a
	}
}

// RecordPromptCall increments the prompt-call counter for the given name.
func (c *MCPStatsCounters) RecordPromptCall(name string) {
	c.PromptCalls.Add(1)
	if v, ok := c.byName[name]; ok {
		v.Add(1)
	} else {
		var a atomic.Int64
		a.Store(1)
		c.byName[name] = &a
	}
}

// RecordSamplingReq increments the sampling request counter.
func (c *MCPStatsCounters) RecordSamplingReq() { c.SamplingReqs.Add(1) }

// RecordElicitationReq increments the elicitation request counter.
func (c *MCPStatsCounters) RecordElicitationReq() { c.ElicitationReqs.Add(1) }

// Snapshot returns an MCPStatsBlock snapshot suitable for JSON serialization.
func (c *MCPStatsCounters) Snapshot() *MCPStatsBlock {
	b := &MCPStatsBlock{
		ResourceReadsTotal:   c.ResourceReads.Load(),
		PromptCallsTotal:     c.PromptCalls.Load(),
		SamplingReqsTotal:    c.SamplingReqs.Load(),
		ElicitationReqsTotal: c.ElicitationReqs.Load(),
	}
	if len(c.byURI) > 0 {
		b.ResourceReadsByURI = make(map[string]int64, len(c.byURI))
		for k, v := range c.byURI {
			b.ResourceReadsByURI[k] = v.Load()
		}
	}
	if len(c.byName) > 0 {
		b.PromptCallsByName = make(map[string]int64, len(c.byName))
		for k, v := range c.byName {
			b.PromptCallsByName[k] = v.Load()
		}
	}
	return b
}
