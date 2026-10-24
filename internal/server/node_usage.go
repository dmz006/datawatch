// node_usage.go — per-compute-node usage gathering for the node:<name>
// capacity pools. For each node it collects the loaded models (ollama
// /api/ps, only for ollama-kind nodes with an address) and the GPU
// figures from the node's observer data (monitoring endpoint, peer
// registry snapshot, or the local observer — in that order). A small TTL
// cache (nodeUsageCache) serves repeated reads without blocking on the
// network and runs at most one background refresh per node at a time.
// handleCapacity attaches one entry to each node:<name> pool as its
// "usage" field.

package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

// node usage timings and limits.
const (
	// nodeUsageTTL is how long a cache entry stays fresh before get()
	// returns it stale-but-immediately and starts one background refresh.
	nodeUsageTTL = 15 * time.Second
	// nodeUsageProbeTimeout bounds a single model or GPU probe.
	nodeUsageProbeTimeout = 2 * time.Second
	// nodeUsageRefreshTimeout bounds the whole background refresh (both
	// probes run concurrently under it).
	nodeUsageRefreshTimeout = 3 * time.Second
	// nodeUsageMaxModels caps how many loaded models are kept per node.
	nodeUsageMaxModels = 8
	// nodeUsageBodyLimit caps response bodies read from node endpoints.
	nodeUsageBodyLimit = 1 << 20
	// nodeUsageErrMaxLen caps a probe error in the response body — short,
	// and never carrying a URL that could embed credentials.
	nodeUsageErrMaxLen = 120
)

// nodeUsageExpiryCutoff: an expires_at at or after this is treated as
// "the model never expires".
var nodeUsageExpiryCutoff = time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC)

// node usage states (JSON "state").
const (
	nodeUsageStateOK          = "ok"
	nodeUsageStateUnreachable = "unreachable"
	nodeUsageStatePending     = "pending"
	nodeUsageStateNoData      = "no_data"
)

// nodeUsage is the per-node usage payload attached to node:<name> pools.
// Models and GPU are always arrays (never null) in JSON.
type nodeUsage struct {
	State           string            `json:"state"`
	CheckedAt       time.Time         `json:"checked_at"`
	Error           string            `json:"error,omitempty"`
	ModelsSupported bool              `json:"models_supported"`
	Models          []nodeLoadedModel `json:"models"`
	GPU             []nodeGPUUsage    `json:"gpu"`
}

// nodeLoadedModel is one resident model reported by ollama /api/ps.
type nodeLoadedModel struct {
	Name          string     `json:"name"`
	SizeBytes     int64      `json:"size_bytes"`
	SizeVRAMBytes int64      `json:"size_vram_bytes"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	NoExpiry      bool       `json:"no_expiry"`
}

// nodeGPUUsage is one GPU's utilisation from the node's observer stats.
type nodeGPUUsage struct {
	Name          string  `json:"name"`
	UtilPct       float64 `json:"util_pct"`
	MemUsedBytes  int64   `json:"mem_used_bytes"`
	MemTotalBytes int64   `json:"mem_total_bytes"`
}

func newPendingNodeUsage() nodeUsage {
	return nodeUsage{State: nodeUsageStatePending, Models: []nodeLoadedModel{}, GPU: []nodeGPUUsage{}}
}

// nodePSModel is one entry of ollama's GET /api/ps models[] (the same
// shape internal/pinkeeper parses).
type nodePSModel struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	Size      int64  `json:"size"`
	SizeVRAM  int64  `json:"size_vram"`
	ExpiresAt string `json:"expires_at"`
}

type nodePSResponse struct {
	Models []nodePSModel `json:"models"`
}

// nodeUsageErr builds a short probe error: at most nodeUsageErrMaxLen
// characters, and callers pass only reason text (never URLs).
func nodeUsageErr(format string, args ...any) error {
	msg := fmt.Sprintf(format, args...)
	if len(msg) > nodeUsageErrMaxLen {
		msg = msg[:nodeUsageErrMaxLen]
	}
	return errors.New(msg)
}

// newNodeUsageClient returns an HTTP client for probing operator-declared
// node endpoints.
func newNodeUsageClient() *http.Client {
	return &http.Client{
		Timeout: nodeUsageProbeTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- operator-declared node URL, often self-signed
		},
	}
}

// probeNodeModels fetches the loaded models from an ollama node's
// /api/ps. probed is false when the node kind has no model probe (not
// ollama, or no address) — that is not an error. Any failure returns a
// short error.
func probeNodeModels(ctx context.Context, client *http.Client, n compute.Node) (models []nodeLoadedModel, probed bool, err error) {
	models = []nodeLoadedModel{}
	if n.Kind != compute.KindOllama || n.Address == "" {
		return models, false, nil
	}
	if client == nil {
		client = newNodeUsageClient()
	}
	req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(n.Address, "/")+"/api/ps", nil)
	if rerr != nil {
		return models, true, nodeUsageErr("model probe: build request failed")
	}
	resp, derr := client.Do(req)
	if derr != nil {
		if te, ok := derr.(interface{ Timeout() bool }); ok && te.Timeout() {
			return models, true, nodeUsageErr("model probe: timed out")
		}
		return models, true, nodeUsageErr("model probe: connection failed")
	}
	defer resp.Body.Close() //nolint:errcheck
	body, _ := io.ReadAll(io.LimitReader(resp.Body, nodeUsageBodyLimit))
	if resp.StatusCode != http.StatusOK {
		return models, true, nodeUsageErr("model probe: HTTP %d", resp.StatusCode)
	}
	var doc nodePSResponse
	if jerr := json.Unmarshal(body, &doc); jerr != nil {
		return models, true, nodeUsageErr("model probe: unparseable response")
	}
	out := []nodeLoadedModel{}
	for _, m := range doc.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		lm := nodeLoadedModel{Name: name, SizeBytes: m.Size, SizeVRAMBytes: m.SizeVRAM}
		raw := strings.TrimSpace(m.ExpiresAt)
		if raw != "" {
			if t, perr := time.Parse(time.RFC3339, raw); perr == nil && !t.IsZero() && t.Before(nodeUsageExpiryCutoff) {
				tv := t
				lm.ExpiresAt = &tv
			} else {
				// Unparseable, zero, or far beyond the cutoff: the model
				// effectively never expires.
				lm.NoExpiry = true
			}
		} else {
			lm.NoExpiry = true
		}
		out = append(out, lm)
		if len(out) >= nodeUsageMaxModels {
			break
		}
	}
	return out, true, nil
}

// nodeUsageStatsDoc is the GPU part of an observer stats payload, used to
// decode both a pulled monitoring endpoint body and the local observer's
// Stats() marshalled to JSON.
type nodeUsageStatsDoc struct {
	GPU []nodeStatsGPUField `json:"gpu"`
}

type nodeStatsGPUField struct {
	Name          string  `json:"name"`
	UtilPct       float64 `json:"util_pct"`
	MemUsedBytes  int64   `json:"mem_used_bytes"`
	MemTotalBytes int64   `json:"mem_total_bytes"`
}

func (d nodeUsageStatsDoc) usages() []nodeGPUUsage {
	out := make([]nodeGPUUsage, 0, len(d.GPU))
	for _, g := range d.GPU {
		out = append(out, nodeGPUUsage{Name: g.Name, UtilPct: g.UtilPct, MemUsedBytes: g.MemUsedBytes, MemTotalBytes: g.MemTotalBytes})
	}
	return out
}

// nodeGPUFromObserver returns the node's GPU usage figures using only the
// node's explicit relationship fields (never host/IP/name resolution):
// (a) the configured monitoring endpoint, (b) the last observer peer push
// for the node's bound peer (ObserverPeer, or the node name when unset),
// (c) the local observer when the bound peer is this daemon and ObserverPeer
// is set explicitly. Any source with no GPU data yields an empty slice.
func (s *Server) nodeGPUFromObserver(ctx context.Context, n compute.Node) []nodeGPUUsage {
	if n.MonitoringEndpoint != "" {
		endpoint := n.MonitoringEndpoint
		if !strings.Contains(endpoint, "/api/") {
			endpoint = strings.TrimRight(endpoint, "/") + "/api/stats"
		}
		client := newNodeUsageClient()
		if req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil); err == nil {
			if resp, err := client.Do(req); err == nil {
				defer resp.Body.Close() //nolint:errcheck
				if resp.StatusCode == http.StatusOK {
					body, _ := io.ReadAll(io.LimitReader(resp.Body, nodeUsageBodyLimit))
					var doc nodeUsageStatsDoc
					if json.Unmarshal(body, &doc) == nil {
						return doc.usages()
					}
				}
			}
		}
	}
	peerName := n.Name
	if n.ObserverPeer != "" {
		peerName = n.ObserverPeer
	}
	if s.peerRegistry != nil {
		if snap := s.peerRegistry.LastPayload(peerName); snap != nil {
			out := make([]nodeGPUUsage, 0, len(snap.GPU))
			for _, g := range snap.GPU {
				out = append(out, nodeGPUUsage{
					Name:          g.Name,
					UtilPct:       g.UtilPct,
					MemUsedBytes:  int64(g.MemUsedBytes),
					MemTotalBytes: int64(g.MemTotalBytes),
				})
			}
			return out
		}
	}
	if s.observerAPI != nil && peerName == s.hostname && n.ObserverPeer != "" {
		raw, err := json.Marshal(s.observerAPI.Stats())
		if err == nil {
			var doc nodeUsageStatsDoc
			if json.Unmarshal(raw, &doc) == nil {
				return doc.usages()
			}
		}
	}
	return []nodeGPUUsage{}
}

// nodeUsageEntry is one node's cache slot.
type nodeUsageEntry struct {
	usage      nodeUsage
	fetchedAt  time.Time
	refreshing bool
}

// nodeUsageCache is the small TTL cache over per-node usage gathers: it
// never blocks readers on the network and runs at most one background
// refresh per node at a time.
type nodeUsageCache struct {
	mu      sync.Mutex
	entries map[string]*nodeUsageEntry
	ttl     time.Duration
	// probeTimeout bounds one model or GPU probe inside a refresh.
	probeTimeout time.Duration
	// refreshTimeout bounds the whole refresh.
	refreshTimeout time.Duration
	// now is injectable for tests.
	now func() time.Time
	// client sends the node probes (injectable for tests).
	client *http.Client
	// gather produces one nodeUsage for a node; tests can replace it with
	// a fake that does no I/O.
	gather func(ctx context.Context, s *Server, n compute.Node) nodeUsage
}

// newNodeUsageCache builds a cache with the production defaults.
func newNodeUsageCache() *nodeUsageCache {
	c := &nodeUsageCache{
		entries:        map[string]*nodeUsageEntry{},
		ttl:            nodeUsageTTL,
		probeTimeout:   nodeUsageProbeTimeout,
		refreshTimeout: nodeUsageRefreshTimeout,
		now:            func() time.Time { return time.Now().UTC() },
		client:         newNodeUsageClient(),
	}
	c.gather = c.gatherUsage
	return c
}

// gatherUsage is the default gather: it runs the model probe and the GPU
// lookup concurrently and assembles the nodeUsage per the state contract.
func (c *nodeUsageCache) gatherUsage(ctx context.Context, s *Server, n compute.Node) nodeUsage {
	overall, cancel := context.WithTimeout(ctx, c.refreshTimeout)
	defer cancel()

	type modelResult struct {
		models []nodeLoadedModel
		probed bool
		err    error
	}
	var mres modelResult
	var gpus []nodeGPUUsage
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		pctx, pcancel := context.WithTimeout(overall, c.probeTimeout)
		defer pcancel()
		mres.models, mres.probed, mres.err = probeNodeModels(pctx, c.client, n)
	}()
	go func() {
		defer wg.Done()
		gctx, gcancel := context.WithTimeout(overall, c.probeTimeout)
		defer gcancel()
		gpus = s.nodeGPUFromObserver(gctx, n)
	}()
	wg.Wait()

	if gpus == nil {
		gpus = []nodeGPUUsage{}
	}
	if mres.models == nil {
		mres.models = []nodeLoadedModel{}
	}

	probeExpected := n.Kind == compute.KindOllama
	u := nodeUsage{
		Models:          mres.models,
		GPU:             gpus,
		ModelsSupported: probeExpected && n.Address != "",
	}
	switch {
	case probeExpected && u.ModelsSupported && mres.err != nil:
		// The model server probe failed; GPU figures, if any, still stand.
		u.State = nodeUsageStateUnreachable
		u.Error = mres.err.Error()
	case probeExpected && !u.ModelsSupported && len(gpus) == 0:
		// No address to probe and no observer data at all.
		u.State = nodeUsageStateNoData
	default:
		// The model server answered, or the node kind has no model probe.
		u.State = nodeUsageStateOK
	}
	return u
}

// refresh does both probes and stores the result under the node name. It
// blocks until the refresh completes (or its timeout elapses).
func (c *nodeUsageCache) refresh(ctx context.Context, s *Server, n compute.Node) nodeUsage {
	now := c.now()
	u := c.gather(ctx, s, n)
	u.CheckedAt = now
	c.mu.Lock()
	e := c.entries[n.Name]
	if e == nil {
		e = &nodeUsageEntry{}
		c.entries[n.Name] = e
	}
	e.usage = u
	e.fetchedAt = now
	e.refreshing = false
	c.mu.Unlock()
	return u
}

// refreshSync is the blocking refresh, exposed so tests and synchronous
// callers can avoid sleeping on a background goroutine.
func (c *nodeUsageCache) refreshSync(ctx context.Context, s *Server, n compute.Node) nodeUsage {
	return c.refresh(ctx, s, n)
}

// get returns the node's usage without ever blocking on the network. With
// no entry it returns a pending placeholder and starts a background
// refresh; with a stale entry it returns the old value immediately and
// starts at most one background refresh per node (never two for the same
// node at once).
func (c *nodeUsageCache) get(s *Server, n compute.Node) nodeUsage {
	c.mu.Lock()
	e := c.entries[n.Name]
	start := false
	if e == nil {
		e = &nodeUsageEntry{usage: newPendingNodeUsage()}
		e.refreshing = true
		c.entries[n.Name] = e
		start = true
	} else if !e.refreshing && !e.fetchedAt.IsZero() && c.now().Sub(e.fetchedAt) > c.ttl {
		e.refreshing = true
		start = true
	}
	// Capture the payload under the lock: the background refresh replaces
	// e.usage concurrently, so reading it after Unlock would race.
	out := e.usage
	c.mu.Unlock()
	if start {
		c.startRefresh(s, n)
	}
	return out
}

// startRefresh launches the background refresh, bounded by the overall
// refresh timeout, and always clears the node's refreshing flag.
func (c *nodeUsageCache) startRefresh(s *Server, n compute.Node) {
	go func() {
		defer func() {
			_ = recover() // never let a panicking probe wedge the cache
			c.mu.Lock()
			if e := c.entries[n.Name]; e != nil {
				e.refreshing = false
			}
			c.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), c.refreshTimeout+time.Second)
		defer cancel()
		_ = c.refresh(ctx, s, n)
	}()
}

// prune drops cache entries for nodes that no longer exist. nil names
// means no current nodes, so everything is dropped.
func (c *nodeUsageCache) prune(names []string) {
	keep := make(map[string]bool, len(names))
	for _, name := range names {
		keep[name] = true
	}
	c.mu.Lock()
	for key := range c.entries {
		if !keep[key] {
			delete(c.entries, key)
		}
	}
	c.mu.Unlock()
}

// nodeUsageCache returns the Server's lazily-created per-node usage cache
// so a bare &Server{} (as used in tests) works without wiring.
func (s *Server) nodeUsageCache() *nodeUsageCache {
	s.nodeUsageCacheOnce.Do(func() { s.nodeUsageCacheVal = newNodeUsageCache() })
	return s.nodeUsageCacheVal
}
