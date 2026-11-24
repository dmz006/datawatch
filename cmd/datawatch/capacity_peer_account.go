package main

// Federation for the account:claude pool. A federated peer that shares this
// daemon's Claude account holds some of the account's slots, so its leases
// count against the account limit here. Only peers an operator opted in count,
// through a claude-code entry's budget peer_window (the same list the per-LLM
// budget uses); no other peer is contacted. A peer that does not answer counts
// 0 and is logged once per outage (fail open).

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/inference"
)

const peerAccountFetchTimeout = 3 * time.Second

type peerAccountPool struct {
	// peers names the peers sharing the account.
	peers func() []string
	// lookup resolves a peer name to its base URL and bearer token.
	lookup func(name string) (url, token string, ok bool)
	// client performs the fetches; nil means a short-timeout client.
	client *http.Client

	mu         sync.Mutex
	held       map[string]int
	failing    map[string]bool
	refreshing bool
}

// Held is the sum of the account:claude leases the opted-in peers hold, from the
// last refresh. It reads only the cache, so the ledger may call it under its lock.
func (p *peerAccountPool) Held() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, h := range p.held {
		n += h
	}
	return n
}

// Refresh reads each peer's own held count for the account pool from its
// GET /api/capacity (the leases the peer holds itself, never its external figure,
// so two peers counting each other cannot double count) and replaces the cache.
func (p *peerAccountPool) Refresh(ctx context.Context) {
	p.mu.Lock()
	if p.refreshing {
		p.mu.Unlock()
		return
	}
	p.refreshing = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.refreshing = false
		p.mu.Unlock()
	}()
	var names []string
	if p.peers != nil {
		names = p.peers()
	}
	type result struct {
		held   int
		reason string
	}
	results := make([]result, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			h, reason := p.fetch(ctx, name)
			results[i] = result{h, reason}
		}(i, name)
	}
	wg.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failing == nil {
		p.failing = map[string]bool{}
	}
	held := make(map[string]int, len(names))
	live := map[string]bool{}
	for i, name := range names {
		live[name] = true
		held[name] = results[i].held
		switch {
		case results[i].reason != "" && !p.failing[name]:
			p.failing[name] = true
			log.Printf("[capacity] peer %s holds of the Claude account not counted: %s", name, results[i].reason)
		case results[i].reason == "":
			delete(p.failing, name)
		}
	}
	for name := range p.failing {
		if !live[name] {
			delete(p.failing, name)
		}
	}
	p.held = held
}

func (p *peerAccountPool) fetch(ctx context.Context, name string) (int, string) {
	if p.lookup == nil {
		return 0, "no peer store"
	}
	base, token, ok := p.lookup(name)
	if !ok || base == "" {
		return 0, "peer not found"
	}
	ctx, cancel := context.WithTimeout(ctx, peerAccountFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/api/capacity", nil)
	if err != nil {
		return 0, "bad peer address"
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := p.client
	if client == nil {
		client = &http.Client{Timeout: peerAccountFetchTimeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "unreachable"
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return 0, "unreachable: HTTP " + http.StatusText(resp.StatusCode)
	}
	var body struct {
		Pools []struct {
			Name string `json:"name"`
			Held int    `json:"held"`
		} `json:"pools"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return 0, "bad reply"
	}
	for _, pl := range body.Pools {
		if pl.Name == accountClaudePool && pl.Held > 0 {
			return pl.Held, ""
		}
	}
	return 0, ""
}

// claudeAccountPeers lists the peers opted in through the budget peer_window of
// any claude-code entry, sorted, each once.
func claudeAccountPeers(registry func() *inference.Registry) func() []string {
	return func() []string {
		reg := registry()
		if reg == nil {
			return nil
		}
		seen := map[string]bool{}
		for _, l := range reg.List() {
			if l.Kind != inference.KindClaudeCode || l.Budget == nil {
				continue
			}
			for _, peer := range l.Budget.PeerWindow {
				if peer != "" {
					seen[peer] = true
				}
			}
		}
		out := make([]string, 0, len(seen))
		for peer := range seen {
			out = append(out, peer)
		}
		sort.Strings(out)
		return out
	}
}
