package compute

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// maxModelListBody caps how much of a node's model-list response is read.
const maxModelListBody = 1 << 20 // 1 MiB

// ErrNoModelList is returned by a ModelLister when a node has no readable
// model list: its kind exposes no model endpoint, or it has no address. The
// caller treats the node's served models as unknown.
var ErrNoModelList = errors.New("node has no model list to read")

// ModelLister reads the names of the models a node serves.
type ModelLister func(ctx context.Context, n *Node, timeout time.Duration) ([]string, error)

// ParseOllamaTags parses an Ollama GET /api/tags body
// ({"models":[{"name":"llama3:70b"}]}). It returns nil when the body is not
// valid JSON and an empty, non-nil slice when it lists no models.
func ParseOllamaTags(b []byte) []string {
	var doc struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Models))
	for _, m := range doc.Models {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out
}

// ParseOpenAIModels parses an OpenAI-compatible model list
// ({"data":[{"id":"gpt-x"}]}), the shape OpenWebUI serves at /api/v1/models.
// It returns nil when the body is not valid JSON and an empty, non-nil slice
// when it lists no models.
func ParseOpenAIModels(b []byte) []string {
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return nil
	}
	out := make([]string, 0, len(doc.Data))
	for _, m := range doc.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out
}

// modelListEndpoint returns the path and parser for a node kind, or ok=false
// when the kind has no model endpoint.
func modelListEndpoint(kind NodeKind) (path string, parse func([]byte) []string, ok bool) {
	switch strings.ToLower(string(kind)) {
	case "ollama", "opencode":
		return "/api/tags", ParseOllamaTags, true
	case "openwebui":
		return "/api/v1/models", ParseOpenAIModels, true
	}
	return "", nil, false
}

// HTTPModelLister returns the real reader. Ollama and opencode nodes are read
// at /api/tags and OpenWebUI nodes at /api/v1/models. Any other kind, or a
// node with no address, yields ErrNoModelList. Each call builds its own
// http.Client from the node's TLS settings and reads at most 1 MiB.
func HTTPModelLister() ModelLister {
	return func(ctx context.Context, n *Node, timeout time.Duration) ([]string, error) {
		if n == nil {
			return nil, errors.New("served models: nil node")
		}
		path, parse, ok := modelListEndpoint(n.Kind)
		if !ok || strings.TrimSpace(n.Address) == "" {
			return nil, ErrNoModelList
		}
		tlsCfg, _, err := NodeTLSConfig(n)
		if err != nil {
			return nil, err
		}
		if timeout <= 0 {
			timeout = 8 * time.Second
		}
		transport := &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		}
		client := &http.Client{Timeout: timeout, Transport: transport}
		defer transport.CloseIdleConnections()

		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		url := strings.TrimRight(strings.TrimSpace(n.Address), "/") + path
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("%s returned HTTP %d", path, resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxModelListBody))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		models := parse(body)
		if models == nil {
			return nil, fmt.Errorf("could not parse %s response", path)
		}
		return models, nil
	}
}

// ServedModels is what one node serves, as read now or taken from the cache.
// Known is false when the models could not be read; Error then says why.
type ServedModels struct {
	Node       string    `json:"node"`
	Kind       string    `json:"kind"`
	Models     []string  `json:"models"`
	Known      bool      `json:"known"`
	Error      string    `json:"error"`
	FetchedAt  time.Time `json:"fetched_at"`
	Cached     bool      `json:"cached"`
	AgeSeconds int       `json:"age_seconds"`
}

// ServedModelsCache reads each node's served models through a ModelLister and
// remembers the answer, success or failure, for a configurable age. The age
// and the read timeout are functions so they are taken live from
// configuration on every call. It is safe for concurrent use.
type ServedModelsCache struct {
	lister  ModelLister
	age     func() time.Duration
	timeout func() time.Duration
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]*servedEntry
}

type servedEntry struct {
	mu  sync.Mutex // serialises reads of one node so a dead node is read once
	val ServedModels
	set bool
}

// NewServedModelsCache builds a cache. A nil age means no caching, a nil
// timeout means 8 seconds, and a nil now means time.Now.
func NewServedModelsCache(lister ModelLister, age func() time.Duration, timeout func() time.Duration, now func() time.Time) *ServedModelsCache {
	if age == nil {
		age = func() time.Duration { return 0 }
	}
	if timeout == nil {
		timeout = func() time.Duration { return 8 * time.Second }
	}
	if now == nil {
		now = time.Now
	}
	return &ServedModelsCache{
		lister:  lister,
		age:     age,
		timeout: timeout,
		now:     now,
		entries: map[string]*servedEntry{},
	}
}

func servedKey(n *Node) string {
	return n.Name + "|" + string(n.Kind) + "|" + n.Address + "|" + n.TLSSettingsKey()
}

func (c *ServedModelsCache) entry(key string) *servedEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.entries[key]
	if e == nil {
		e = &servedEntry{}
		c.entries[key] = e
	}
	return e
}

// Get returns what node n serves. Within the cache age the remembered answer
// is returned with Cached set. refresh=true skips the remembered answer and
// replaces it. An age of 0 never remembers anything.
func (c *ServedModelsCache) Get(ctx context.Context, n *Node, refresh bool) ServedModels {
	if n == nil {
		return ServedModels{Models: []string{}, Error: "nil node"}
	}
	age := c.age()
	if age <= 0 {
		return c.fetch(ctx, n)
	}
	e := c.entry(servedKey(n))
	e.mu.Lock()
	defer e.mu.Unlock()
	if !refresh && e.set {
		if elapsed := c.now().Sub(e.val.FetchedAt); elapsed >= 0 && elapsed < age {
			out := cloneServed(e.val)
			out.Cached = true
			out.AgeSeconds = int(elapsed / time.Second)
			return out
		}
	}
	fresh := c.fetch(ctx, n)
	e.val, e.set = cloneServed(fresh), true
	return fresh
}

// GetAll returns what each node serves, ordered by node name. Nodes are read
// concurrently.
func (c *ServedModelsCache) GetAll(ctx context.Context, nodes []*Node, refresh bool) []ServedModels {
	list := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		if n != nil {
			list = append(list, n)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	out := make([]ServedModels, len(list))
	var wg sync.WaitGroup
	for i, n := range list {
		wg.Add(1)
		go func(i int, n *Node) {
			defer wg.Done()
			out[i] = c.Get(ctx, n, refresh)
		}(i, n)
	}
	wg.Wait()
	return out
}

// fetch reads the node now, without touching the cache.
func (c *ServedModelsCache) fetch(ctx context.Context, n *Node) ServedModels {
	res := ServedModels{Node: n.Name, Kind: string(n.Kind), Models: []string{}, FetchedAt: c.now()}
	models, err := c.lister(ctx, n, c.timeout())
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Known = true
	res.Models = append(res.Models, models...)
	return res
}

func cloneServed(s ServedModels) ServedModels {
	s.Models = append([]string{}, s.Models...)
	return s
}
