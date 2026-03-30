// Package stats — ChannelTracker provides atomic per-channel message counters.
package stats

import (
	"sync"
	"sync/atomic"
	"time"
)

// ChannelCounters holds atomic counters for a single communication channel.
type ChannelCounters struct {
	MsgSent   atomic.Int64
	MsgRecv   atomic.Int64
	Errors    atomic.Int64
	BytesIn   atomic.Int64
	BytesOut  atomic.Int64
	LastActivity atomic.Int64 // unix timestamp
}

// RecordSent increments the sent counter and bytes out.
func (c *ChannelCounters) RecordSent(byteCount int) {
	c.MsgSent.Add(1)
	c.BytesOut.Add(int64(byteCount))
	c.LastActivity.Store(time.Now().Unix())
}

// RecordRecv increments the received counter and bytes in.
func (c *ChannelCounters) RecordRecv(byteCount int) {
	c.MsgRecv.Add(1)
	c.BytesIn.Add(int64(byteCount))
	c.LastActivity.Store(time.Now().Unix())
}

// RecordError increments the error counter.
func (c *ChannelCounters) RecordError() {
	c.Errors.Add(1)
	c.LastActivity.Store(time.Now().Unix())
}

// Snapshot returns a point-in-time copy of the counters.
func (c *ChannelCounters) Snapshot() ChannelSnapshot {
	return ChannelSnapshot{
		MsgSent:      int(c.MsgSent.Load()),
		MsgRecv:      int(c.MsgRecv.Load()),
		Errors:       int(c.Errors.Load()),
		BytesIn:      c.BytesIn.Load(),
		BytesOut:     c.BytesOut.Load(),
		LastActivity: c.LastActivity.Load(),
	}
}

// ChannelSnapshot is a non-atomic copy of channel counters.
type ChannelSnapshot struct {
	MsgSent      int
	MsgRecv      int
	Errors       int
	BytesIn      int64
	BytesOut     int64
	LastActivity int64 // unix timestamp, 0 if never
}

// ChannelTracker manages per-channel counters. Thread-safe.
type ChannelTracker struct {
	mu       sync.RWMutex
	channels map[string]*ChannelCounters
}

// NewChannelTracker creates a new tracker.
func NewChannelTracker() *ChannelTracker {
	return &ChannelTracker{
		channels: make(map[string]*ChannelCounters),
	}
}

// Get returns the counters for a channel, creating if needed.
func (t *ChannelTracker) Get(name string) *ChannelCounters {
	t.mu.RLock()
	c, ok := t.channels[name]
	t.mu.RUnlock()
	if ok {
		return c
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// Double-check after write lock
	if c, ok = t.channels[name]; ok {
		return c
	}
	c = &ChannelCounters{}
	t.channels[name] = c
	return c
}

// Snapshot returns all channel snapshots.
func (t *ChannelTracker) Snapshot() map[string]ChannelSnapshot {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make(map[string]ChannelSnapshot, len(t.channels))
	for name, c := range t.channels {
		out[name] = c.Snapshot()
	}
	return out
}
