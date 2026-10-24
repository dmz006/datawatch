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
}

// HostLister returns the Ollama endpoints of this host and of the compute
// nodes.
type HostLister interface {
	PoolHosts(ctx context.Context) []PoolHost
}

// ModelFetcher returns the names of the models loaded on one endpoint.
type ModelFetcher interface {
	LoadedModels(ctx context.Context, endpoint string) ([]string, error)
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

type poolEvidenceSource struct {
	sessions SessionLister
	hosts    HostLister
	fetch    ModelFetcher
	backend  func() string
	now      func() time.Time

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
// When a host cannot be read, Check returns the partial evidence and an error
// that names the host, so the planner never calls the pool idle on a guess.
// A result is reused for at most 5 seconds.
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
			faults = append(faults, hostFault(f.Host, "session list", f.Err))
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
			results[i].models, results[i].err = s.fetch.LoadedModels(ctx, h.Endpoint)
		}(i, h)
	}
	wg.Wait()
	for _, r := range results {
		if r.err != nil {
			faults = append(faults, hostFault(r.host.Name, "loaded models", r.err))
			continue
		}
		for _, m := range r.models {
			ev.LoadedModels = append(ev.LoadedModels, LoadedModel{Host: r.host.Name, Model: m})
		}
	}
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

// httpModelFetcher reads the loaded models of an Ollama endpoint with a GET of
// /api/ps.
type httpModelFetcher struct {
	client   *http.Client
	timeout  time.Duration
	maxBytes int64
}

// NewHTTPModelFetcher returns the ModelFetcher that asks an Ollama endpoint's
// /api/ps with a short timeout and a bounded response. Its errors never carry
// the endpoint URL, so credentials in it stay out of logs.
func NewHTTPModelFetcher(client *http.Client) ModelFetcher {
	if client == nil {
		client = &http.Client{}
	}
	return &httpModelFetcher{client: client, timeout: poolPSTimeout, maxBytes: poolPSMaxBytes}
}

func (f *httpModelFetcher) LoadedModels(ctx context.Context, endpoint string) ([]string, error) {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" {
		return nil, errors.New("no endpoint")
	}
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/ps", nil)
	if err != nil {
		return nil, errors.New("ollama /api/ps: invalid endpoint")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err // drops the URL
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
