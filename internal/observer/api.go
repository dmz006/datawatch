// BL171 (S9) — adapter that lets the server package consume the
// observer Collector through a narrow interface (no import cycle).

package observer

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// API wraps a *Collector for REST / MCP / CLI consumers.
type API struct{ C *Collector }

func NewAPI(c *Collector) *API { return &API{C: c} }

func (a *API) Config() any { return a.C.Config() }

func (a *API) SetConfig(v any) error {
	raw, ok := v.(json.RawMessage)
	if !ok {
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("config: %w", err)
		}
		raw = b
	}
	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	a.C.SetConfig(cfg)
	return nil
}

// Stats returns the latest snapshot. Never nil after Start.
func (a *API) Stats() any { return a.C.Latest() }

// Envelopes returns just the envelope rollup — convenient when the
// caller doesn't care about the process tree.
func (a *API) Envelopes() any {
	snap := a.C.Latest()
	if snap == nil {
		return []Envelope{}
	}
	return snap.Envelopes
}

// EnvelopeTree returns the process sub-tree filtered to one
// envelope ID (e.g. "session:ralfthewise-787e"). Returns nil when
// the envelope isn't present in the current snapshot.
func (a *API) EnvelopeTree(id string) any {
	snap := a.C.Latest()
	if snap == nil {
		return nil
	}
	// Find the matching envelope and collect its pids.
	var pidSet map[int]bool
	for _, e := range snap.Envelopes {
		if e.ID == id {
			pidSet = map[int]bool{}
			for _, p := range e.PIDs {
				pidSet[p] = true
			}
			break
		}
	}
	if pidSet == nil {
		return nil
	}
	// Filter the process tree to nodes whose pid is in the envelope.
	var filter func([]ProcessNode) []ProcessNode
	filter = func(ns []ProcessNode) []ProcessNode {
		var out []ProcessNode
		for _, n := range ns {
			kids := filter(n.Children)
			if pidSet[n.PID] || len(kids) > 0 {
				n.Children = kids
				out = append(out, n)
			}
		}
		return out
	}
	return map[string]any{
		"envelope_id": id,
		"tree":        filter(snap.Processes.Tree),
	}
}

// Start delegates to the underlying Collector.
func (a *API) Start(ctx context.Context) { a.C.Start(ctx) }

// Stop delegates.
func (a *API) Stop() { a.C.Stop() }

// EnvelopeExists is a convenience for handler validation.
func (a *API) EnvelopeExists(id string) bool {
	if snap := a.C.Latest(); snap != nil {
		for _, e := range snap.Envelopes {
			if e.ID == id {
				return true
			}
		}
	}
	return false
}

// IDPrefixMatches returns envelopes whose ID starts with the given
// prefix (e.g. all "backend:" envelopes). Convenience for CLI
// filters.
func (a *API) IDPrefixMatches(prefix string) []Envelope {
	snap := a.C.Latest()
	if snap == nil {
		return nil
	}
	var out []Envelope
	for _, e := range snap.Envelopes {
		if strings.HasPrefix(e.ID, prefix) {
			out = append(out, e)
		}
	}
	return out
}
