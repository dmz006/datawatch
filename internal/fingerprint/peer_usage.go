package fingerprint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	defaultPeerUsageTTL     = 10 * time.Second
	defaultPeerUsageTimeout = 3 * time.Second
)

// peerUsageEntry is one peer's last good /api/capacity answer: every
// pool's held count and when it was fetched.
type peerUsageEntry struct {
	held    map[string]int
	fetched time.Time
}

// PeerUsageCache keeps the last known held counts of federated peers so
// the admission path reads them from memory. Fetching happens only in
// Refresh, RefreshAll and the background goroutine started by Request;
// the cache mutex is never held across a network call.
type PeerUsageCache struct {
	Peers  func() []PeerRef
	Client *http.Client  // nil = a client with a 3-second timeout
	TTL    time.Duration // zero = 10 seconds
	MaxAge time.Duration // zero = 5 times TTL; older entries count as missing
	Now    func() time.Time

	mu          sync.Mutex
	entries     map[string]peerUsageEntry
	inflight    map[string]bool
	unreachable map[string]bool // peers currently in a failure episode
}

func (c *PeerUsageCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *PeerUsageCache) ttl() time.Duration {
	if c.TTL > 0 {
		return c.TTL
	}
	return defaultPeerUsageTTL
}

func (c *PeerUsageCache) maxAge() time.Duration {
	if c.MaxAge > 0 {
		return c.MaxAge
	}
	return 5 * c.ttl()
}

func (c *PeerUsageCache) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: defaultPeerUsageTimeout}
}

// Held returns the cached held count of pool on peer. It is a pure
// in-memory read: a missing or too-old entry, or a pool the peer did not
// report, gives (0, false). It never does I/O.
func (c *PeerUsageCache) Held(peer, pool string) (int, bool) {
	c.mu.Lock()
	e, ok := c.entries[peer]
	c.mu.Unlock()
	if !ok || c.now().Sub(e.fetched) > c.maxAge() {
		return 0, false
	}
	n, ok := e.held[pool]
	return n, ok
}

// stale reports whether peer has no entry or one older than the TTL.
func (c *PeerUsageCache) stale(peer string) bool {
	c.mu.Lock()
	e, ok := c.entries[peer]
	c.mu.Unlock()
	return !ok || c.now().Sub(e.fetched) > c.ttl()
}

// claim marks peer as having a fetch in flight; false means one already is.
func (c *PeerUsageCache) claim(peer string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inflight[peer] {
		return false
	}
	if c.inflight == nil {
		c.inflight = make(map[string]bool)
	}
	c.inflight[peer] = true
	return true
}

func (c *PeerUsageCache) release(peer string) {
	c.mu.Lock()
	delete(c.inflight, peer)
	c.mu.Unlock()
}

// Refresh fetches peer's /api/capacity and stores every pool's held
// count. If a fetch for the same peer is already running it returns nil
// at once without a request. A failure is returned and logged once per
// episode; the previous entry is kept and simply ages out.
func (c *PeerUsageCache) Refresh(ctx context.Context, peer PeerRef) error {
	if !c.claim(peer.Name) {
		return nil
	}
	return c.fetch(ctx, peer)
}

// fetch runs a claimed fetch and releases the claim.
func (c *PeerUsageCache) fetch(ctx context.Context, peer PeerRef) error {
	defer c.release(peer.Name)
	held, err := c.get(ctx, peer)
	if err != nil {
		c.failOnce(peer.Name, err)
		return err
	}
	c.mu.Lock()
	if c.entries == nil {
		c.entries = make(map[string]peerUsageEntry)
	}
	c.entries[peer.Name] = peerUsageEntry{held: held, fetched: c.now()}
	delete(c.unreachable, peer.Name)
	c.mu.Unlock()
	return nil
}

// get performs the HTTP request and parses the pools.
func (c *PeerUsageCache) get(ctx context.Context, peer PeerRef) (map[string]int, error) {
	url := strings.TrimSuffix(peer.URL, "/") + "/api/capacity"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if peer.Token != "" {
		req.Header.Set("Authorization", "Bearer "+peer.Token)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var view struct {
		Pools []capacityPoolView `json:"pools"`
	}
	if err := json.Unmarshal(body, &view); err != nil {
		return nil, err
	}
	held := make(map[string]int, len(view.Pools))
	for _, p := range view.Pools {
		held[p.Name] = p.Held
	}
	return held, nil
}

// RefreshAll refreshes every opted-in peer; peers not opted in get no call.
func (c *PeerUsageCache) RefreshAll(ctx context.Context) {
	if c.Peers == nil {
		return
	}
	for _, p := range c.Peers() {
		if p.OptedIn {
			_ = c.Refresh(ctx, p)
		}
	}
}

// Request is the on-demand hook: when peer's entry is older than the TTL
// (or missing) and no fetch is in flight, it starts one background fetch
// with a bounded context and returns immediately.
func (c *PeerUsageCache) Request(peer PeerRef) {
	if !peer.OptedIn || !c.stale(peer.Name) || !c.claim(peer.Name) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*defaultPeerUsageTimeout)
		defer cancel()
		_ = c.fetch(ctx, peer)
	}()
}

// failOnce marks peer as being in a failure episode and logs it the
// first time the episode is seen; repeated failures of the same peer stay
// silent until the peer answers successfully again.
func (c *PeerUsageCache) failOnce(peer string, why error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unreachable == nil {
		c.unreachable = make(map[string]bool)
	}
	if c.unreachable[peer] {
		return
	}
	c.unreachable[peer] = true
	log.Printf("[fingerprint] peer %s shared-pool check failed (%v); failing open, silencing until it answers again", peer, why)
}
