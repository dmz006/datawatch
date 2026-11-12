// Live pool evidence for lane fill: how many sessions are using the GPU pool
// and which models are resident, read from the session lists of this host and
// its peers and from each pool host's Ollama /api/ps.

package autonomous

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dmz006/datawatch/internal/compute"
)

const (
	// poolEvidenceCacheTTL is the longest a result is reused, so a poll and a
	// status read do not hit the hosts twice.
	poolEvidenceCacheTTL = 5 * time.Second
	// poolPSTimeout bounds one /api/ps request.
	poolPSTimeout = 5 * time.Second
	// poolPSMaxBytes bounds the /api/ps response body read.
	poolPSMaxBytes = 1 << 20
)

// PoolSession is one running session as the evidence source sees it.
type PoolSession struct {
	ID string
	// Backend is the backend family the session runs in.
	Backend string
	// Host is the datawatch host the session runs on (empty for this host).
	Host string
	// Node is the compute node the session talks to, when its record says.
	Node string
}

// PoolHostFault names a host whose session list could not be read.
type PoolHostFault struct {
	Host string
	Err  error
}

// SessionLister returns the running sessions of this host and of every peer.
// A host it cannot read is reported in the faults and its sessions are left
// out.
type SessionLister interface {
	RunningSessions(ctx context.Context) ([]PoolSession, []PoolHostFault)
}

// PoolHost is one Ollama endpoint of the pool.
type PoolHost struct {
	Name     string
	Endpoint string
	// Node is the name of the registered compute node this host is, so the
	// fetcher can read the node's own TLS settings. It is empty for the host's
	// local Ollama, which is read with the default client.
	Node string
}

// HostLister returns the Ollama endpoints of this host and of the compute
// nodes.
type HostLister interface {
	PoolHosts(ctx context.Context) []PoolHost
}

// ModelFetcher returns the names of the models loaded on one pool host. It is
// given the whole host, not just the endpoint, so it can read a registered
// node with that node's own TLS settings.
type ModelFetcher interface {
	LoadedModels(ctx context.Context, host PoolHost) ([]string, error)
}

// PoolEvidenceOption adjusts a pool evidence source.
type PoolEvidenceOption func(*poolEvidenceSource)

// PoolEvidenceGPUBackend sets where the source reads the configured GPU
// backend, live on each check. The default is the shipped gpu_backend.
func PoolEvidenceGPUBackend(fn func() string) PoolEvidenceOption {
	return func(s *poolEvidenceSource) {
		if fn != nil {
			s.backend = fn
		}
	}
}

// PoolEvidenceClock sets the clock the cache uses (tests).
func PoolEvidenceClock(now func() time.Time) PoolEvidenceOption {
	return func(s *poolEvidenceSource) {
		if now != nil {
			s.now = now
		}
	}
}

// PoolHostDegraded is a pool host whose loaded models could not be read but
// whose failure does not block lane planning: it holds no work and has been
// unreachable past the host-down threshold. It is a warning, never an error.
type PoolHostDegraded struct {
	Host string
	// Error is the failure text, without the endpoint URL.
	Error string
	// Since is when the host first failed, counting only consecutive failed
	// reads.
	Since time.Time
}

// HostHolders names the pool hosts that currently hold work.
type HostHolders interface {
	// HeldHosts returns the set of pool host names that hold a session lease
	// or an Automaton placement, are named as the node of a running session,
	// or are a node a lane's work is placed on.
	HeldHosts(ctx context.Context) map[string]bool
}

// PoolEvidenceHostDownAfter sets where the source reads the host-down
// threshold in minutes, live on each check. 0 means never treat an unreachable
// host as down, so every failed read blocks. Without this option the threshold
// is 0.
func PoolEvidenceHostDownAfter(fn func() int) PoolEvidenceOption {
	return func(s *poolEvidenceSource) {
		if fn != nil {
			s.downAfter = fn
		}
	}
}

// PoolEvidenceHolders sets the source of the hosts that hold work. With none,
// nothing is known to be held, but the not-yet-down rule and the zero
// threshold still apply.
func PoolEvidenceHolders(h HostHolders) PoolEvidenceOption {
	return func(s *poolEvidenceSource) { s.holders = h }
}

type poolEvidenceSource struct {
	sessions  SessionLister
	hosts     HostLister
	fetch     ModelFetcher
	backend   func() string
	now       func() time.Time
	downAfter func() int
	holders   HostHolders

	// firstFail is, per pool host, the time of its first consecutive failed
	// read. It is cleared by a successful read and when the host leaves the
	// pool. Guarded by mu.
	firstFail map[string]time.Time

	mu       sync.Mutex
	cached   bool
	cachedAt time.Time
	key      string
	ev       PoolEvidence
	err      error
}

// NewPoolEvidenceSource builds the source lane fill reads the pool with.
//
// ActiveGPUSessions counts the running sessions that use the pool: those on
// the configured gpu_backend, and those of any other backend whose record
// names a pool host as the node they talk to. A resident model alone does not
// count as busy; loaded models are only reported.
//
// A failed read of a host's loaded models is blocking, and Check returns the
// partial evidence and an error that names the host, so the planner never
// calls the pool idle on a guess, when any of these holds: the host holds work
// (a lease, an Automaton placement, a running session naming it, or a lane
// node, see HostHolders); the host has been unreachable for less than the
// host-down threshold (PoolEvidenceHostDownAfter); or the threshold is 0.
// Otherwise the host is degraded: the failure is left out of the error, the
// lanes are judged from the readable hosts, and the host is listed in
// PoolEvidence.Degraded with the error text and the time it first failed. A
// failed session list on this host or a pool host always blocks. A result,
// its Degraded list included, is reused for at most 5 seconds.
func NewPoolEvidenceSource(sessions SessionLister, hosts HostLister, fetch ModelFetcher, opts ...PoolEvidenceOption) PoolEvidenceSource {
	s := &poolEvidenceSource{
		sessions: sessions, hosts: hosts, fetch: fetch,
		backend: func() string { return DefaultLaneFillConfig().EffectiveGPUBackend() },
		now:     time.Now,
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Check reads the pool, or returns the cached reading.
func (s *poolEvidenceSource) Check(ctx context.Context) (PoolEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(s.backend())
	now := s.now()
	if s.cached && s.key == key {
		if age := now.Sub(s.cachedAt); age >= 0 && age < poolEvidenceCacheTTL {
			return s.ev, s.err
		}
	}
	ev, err := s.read(ctx, key, now)
	s.cached, s.cachedAt, s.key, s.ev, s.err = true, now, key, ev, err
	return ev, err
}

func (s *poolEvidenceSource) read(ctx context.Context, gpuBackend string, now time.Time) (PoolEvidence, error) {
	ev := PoolEvidence{CheckedAt: now, LoadedModels: []LoadedModel{}}
	var faults []error

	var hosts []PoolHost
	if s.hosts != nil {
		hosts = s.hosts.PoolHosts(ctx)
	}
	poolNodes := make(map[string]bool, len(hosts))
	for _, h := range hosts {
		poolNodes[h.Name] = true
	}

	if s.sessions == nil {
		faults = append(faults, errors.New("session list unavailable"))
	} else {
		sessions, sessFaults := s.sessions.RunningSessions(ctx)
		for _, f := range sessFaults {
			// Only faults on the local host or a pool host can hide GPU
			// work; an unrelated federation peer failing says nothing
			// about this pool.
			if f.Host == "" || f.Host == "local" || poolNodes[f.Host] {
				faults = append(faults, hostFault(f.Host, "session list", f.Err))
			}
		}
		for _, sess := range sessions {
			if (gpuBackend != "" && sess.Backend == gpuBackend) || (sess.Node != "" && poolNodes[sess.Node]) {
				ev.ActiveGPUSessions++
			}
		}
	}

	type fetched struct {
		host   PoolHost
		models []string
		err    error
	}
	results := make([]fetched, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		results[i].host = h
		if s.fetch == nil {
			results[i].err = errors.New("model fetcher unavailable")
			continue
		}
		wg.Add(1)
		go func(i int, h PoolHost) {
			defer wg.Done()
			results[i].models, results[i].err = s.fetch.LoadedModels(ctx, h)
		}(i, h)
	}
	wg.Wait()

	// Forget the first-failure time of a host that left the pool.
	for name := range s.firstFail {
		if !poolNodes[name] {
			delete(s.firstFail, name)
		}
	}
	downAfter := 0
	if s.downAfter != nil {
		downAfter = s.downAfter()
	}
	var held map[string]bool
	heldRead := false
	for _, r := range results {
		name := r.host.Name
		if r.err == nil {
			delete(s.firstFail, name)
			for _, m := range r.models {
				ev.LoadedModels = append(ev.LoadedModels, LoadedModel{Host: name, Model: m})
			}
			continue
		}
		if s.firstFail == nil {
			s.firstFail = map[string]time.Time{}
		}
		first, ok := s.firstFail[name]
		if !ok {
			first = now
			s.firstFail[name] = first
		}
		blocking := downAfter <= 0 || now.Sub(first) < time.Duration(downAfter)*time.Minute
		if !blocking {
			if !heldRead {
				heldRead = true
				if s.holders != nil {
					held = s.holders.HeldHosts(ctx)
				}
			}
			blocking = held[name]
		}
		if blocking {
			faults = append(faults, hostFault(name, "loaded models", r.err))
			continue
		}
		ev.Degraded = append(ev.Degraded, PoolHostDegraded{Host: name, Error: hostFault(name, "loaded models", r.err).Error(), Since: first})
	}
	sort.SliceStable(ev.Degraded, func(i, j int) bool { return ev.Degraded[i].Host < ev.Degraded[j].Host })
	sort.SliceStable(ev.LoadedModels, func(i, j int) bool {
		a, b := ev.LoadedModels[i], ev.LoadedModels[j]
		if a.Host != b.Host {
			return a.Host < b.Host
		}
		return a.Model < b.Model
	})
	return ev, errors.Join(faults...)
}

func hostFault(host, what string, err error) error {
	if host == "" {
		host = "local"
	}
	return fmt.Errorf("pool host %s unreadable (%s): %w", host, what, err)
}

// NodeLookup resolves the live record of a registered compute node.
type NodeLookup func(name string) (*compute.Node, bool)

// httpModelFetcher reads the loaded models of an Ollama endpoint with a GET of
// /api/ps.
type httpModelFetcher struct {
	client   *http.Client
	timeout  time.Duration
	maxBytes int64
	lookup   NodeLookup

	mu         sync.Mutex
	nodeClient map[string]*poolNodeClient
}

// poolNodeClient is the cached client of one https node, valid for as long as
// the node's TLS settings keep the same key.
type poolNodeClient struct {
	key    string
	client *http.Client
}

// NewHTTPModelFetcher returns the ModelFetcher that asks an Ollama endpoint's
// /api/ps with a short timeout and a bounded response. Its errors never carry
// the endpoint URL, so credentials in it stay out of logs.
func NewHTTPModelFetcher(client *http.Client) ModelFetcher {
	return NewHTTPModelFetcherForNodes(client, nil)
}

// NewHTTPModelFetcherForNodes is NewHTTPModelFetcher for a pool that includes
// registered compute nodes. A host that names a node with an https endpoint is
// read with a client built from that node's own TLS settings (tls_ca_file,
// tls_cert_fingerprint or tls_insecure_skip_verify), cached per node and
// rebuilt when those settings change. Plain http nodes and hosts that name no
// node use client (the default client when nil).
func NewHTTPModelFetcherForNodes(client *http.Client, lookup NodeLookup) ModelFetcher {
	if client == nil {
		client = &http.Client{}
	}
	return &httpModelFetcher{client: client, timeout: poolPSTimeout, maxBytes: poolPSMaxBytes, lookup: lookup}
}

// clientFor picks the client for one read of host.
func (f *httpModelFetcher) clientFor(host PoolHost, endpoint string) (*http.Client, error) {
	name := strings.TrimSpace(host.Node)
	if name == "" || f.lookup == nil {
		return f.client, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.lookup(name)
	if !ok || rec == nil || !strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		f.dropLocked(name)
		return f.client, nil
	}
	key := rec.TLSSettingsKey()
	if c := f.nodeClient[name]; c != nil && c.key == key {
		return c.client, nil
	}
	f.dropLocked(name)
	cfg, _, err := compute.NodeTLSConfig(rec)
	if err != nil {
		return nil, fmt.Errorf("node %s TLS settings unusable: %w", name, err)
	}
	var tr *http.Transport
	if dt, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = dt.Clone()
	} else {
		tr = &http.Transport{}
	}
	tr.TLSClientConfig = cfg
	c := &http.Client{Transport: tr}
	if f.nodeClient == nil {
		f.nodeClient = map[string]*poolNodeClient{}
	}
	f.nodeClient[name] = &poolNodeClient{key: key, client: c}
	return c, nil
}

// dropLocked forgets node's cached client. The caller holds f.mu.
func (f *httpModelFetcher) dropLocked(name string) {
	if c := f.nodeClient[name]; c != nil {
		if tr, ok := c.client.Transport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
		delete(f.nodeClient, name)
	}
}

func (f *httpModelFetcher) LoadedModels(ctx context.Context, host PoolHost) ([]string, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(host.Endpoint), "/")
	if endpoint == "" {
		return nil, errors.New("no endpoint")
	}
	client, err := f.clientFor(host, endpoint)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/ps", nil)
	if err != nil {
		return nil, errors.New("ollama /api/ps: invalid endpoint")
	}
	resp, err := client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // drops the URL
		}
		if supervisorWatchCertError(err) {
			if node := strings.TrimSpace(host.Node); node != "" {
				return nil, fmt.Errorf("ollama /api/ps: node %s certificate not trusted: %v: "+
					"set tls_ca_file or tls_cert_fingerprint on the node, or tls_insecure_skip_verify to turn verification off for it", node, err)
			}
		}
		return nil, fmt.Errorf("ollama /api/ps: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama /api/ps: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, f.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("ollama /api/ps: read: %w", err)
	}
	if int64(len(body)) > f.maxBytes {
		return nil, fmt.Errorf("ollama /api/ps: response larger than %d bytes", f.maxBytes)
	}
	var parsed struct {
		Models []struct {
			Name  string `json:"name"`
			Model string `json:"model"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.New("ollama /api/ps: invalid response")
	}
	out := make([]string, 0, len(parsed.Models))
	for _, m := range parsed.Models {
		name := m.Name
		if name == "" {
			name = m.Model
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out, nil
}
