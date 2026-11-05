package classifier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// State is the tri-state capability of one path on one node for one model.
type State string

const (
	// StateUnknown means the probe could not decide (transport error, timeout,
	// server error). It is never treated as supported.
	StateUnknown State = "unknown"
	// StateSupported means the path answered with the pinned shape.
	StateSupported State = "supported"
	// StateUnsupported means the node answered that the path does not exist or
	// does not return the needed data.
	StateUnsupported State = "unsupported"
)

// Path names a classification read path.
type Path string

const (
	// PathNative is POST /v1/systemone.
	PathNative Path = "native"
	// PathLogprobs is chat completions with first-token logprobs.
	PathLogprobs Path = "logprobs"
)

// Config holds the tunables. Config keys (documented in docs/implementation.md):
// classifier.probe_ttl_seconds (default 600) and classifier.probe_timeout_seconds
// (default 5).
type Config struct {
	ProbeTTL     time.Duration
	ProbeTimeout time.Duration
}

// Default values for Config.
const (
	DefaultProbeTTL     = 10 * time.Minute
	DefaultProbeTimeout = 5 * time.Second
)

func (c Config) withDefaults() Config {
	if c.ProbeTTL <= 0 {
		c.ProbeTTL = DefaultProbeTTL
	}
	if c.ProbeTimeout <= 0 {
		c.ProbeTimeout = DefaultProbeTimeout
	}
	return c
}

// CapabilityError is returned when a path is not known to be supported. The
// caller must turn it into a block with the "infra" category; there is no
// silent fallback to generate-and-parse.
type CapabilityError struct {
	Node, Model string
	Path        Path
	State       State
	Cause       error
}

func (e *CapabilityError) Error() string {
	s := fmt.Sprintf("classifier: %s path is %s on node %q for model %q", e.Path, e.State, e.Node, e.Model)
	if e.Cause != nil {
		s += ": " + e.Cause.Error()
	}
	return s
}

// Unwrap returns the probe's underlying cause, if any.
func (e *CapabilityError) Unwrap() error { return e.Cause }

// IsCapabilityError reports whether err is (or wraps) a CapabilityError.
func IsCapabilityError(err error) bool {
	var ce *CapabilityError
	return errors.As(err, &ce)
}

// HTTPDoer is the injectable HTTP client.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// BaseURLFunc resolves a node name to its inference base URL.
type BaseURLFunc func(node string) (string, error)

type probeKey struct {
	node, model string
	path        Path
}

type probeEntry struct {
	state   State
	expires time.Time
}

// Prober probes and caches capability per (node, model, path).
type Prober struct {
	client  HTTPDoer
	baseURL BaseURLFunc
	cfg     Config
	now     func() time.Time

	mu    sync.Mutex
	cache map[probeKey]probeEntry
}

// NewProber builds a Prober. A nil client uses http.DefaultClient; a nil now
// uses time.Now.
func NewProber(client HTTPDoer, baseURL BaseURLFunc, cfg Config, now func() time.Time) *Prober {
	if client == nil {
		client = http.DefaultClient
	}
	if now == nil {
		now = time.Now
	}
	return &Prober{client: client, baseURL: baseURL, cfg: cfg.withDefaults(), now: now, cache: map[probeKey]probeEntry{}}
}

// Probe returns the capability state of path on node for model. Supported and
// unsupported results are cached for the TTL; unknown results are never cached
// so the next call probes again. The error is the probe's cause for unknown.
func (p *Prober) Probe(ctx context.Context, node, model string, path Path) (State, error) {
	k := probeKey{node, model, path}
	p.mu.Lock()
	if e, ok := p.cache[k]; ok && p.now().Before(e.expires) {
		p.mu.Unlock()
		return e.state, nil
	}
	p.mu.Unlock()

	state, err := p.probe(ctx, node, model, path)
	if state != StateUnknown {
		p.mu.Lock()
		p.cache[k] = probeEntry{state, p.now().Add(p.cfg.ProbeTTL)}
		p.mu.Unlock()
	}
	return state, err
}

// Require probes and returns a *CapabilityError unless the state is supported.
func (p *Prober) Require(ctx context.Context, node, model string, path Path) error {
	state, err := p.Probe(ctx, node, model, path)
	if state == StateSupported {
		return nil
	}
	return &CapabilityError{Node: node, Model: model, Path: path, State: state, Cause: err}
}

// Invalidate drops every cached result for a node, e.g. after an upgrade.
func (p *Prober) Invalidate(node string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k := range p.cache {
		if k.node == node {
			delete(p.cache, k)
		}
	}
}

func (p *Prober) probe(ctx context.Context, node, model string, path Path) (State, error) {
	base, err := p.baseURL(node)
	if err != nil {
		return StateUnknown, fmt.Errorf("resolve node %q: %w", node, err)
	}
	var url string
	var body any
	switch path {
	case PathNative:
		url = base + "/v1/systemone"
		body = SystemOneRequest{Model: model, Prompt: "Answer A or B.\nA) yes\nB) no\nAnswer:",
			Questions: []SystemOneQuestion{{Name: "probe", Options: []string{"A", "B"}}}}
	case PathLogprobs:
		url = base + "/v1/chat/completions"
		body = map[string]any{"model": model, "max_tokens": 1, "logprobs": true, "top_logprobs": 2,
			"messages": []map[string]string{{"role": "user", "content": "Answer A or B.\nA) yes\nB) no\nAnswer:"}}}
	default:
		return StateUnknown, fmt.Errorf("unknown path %q", path)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return StateUnknown, err
	}
	ctx, cancel := context.WithTimeout(ctx, p.cfg.ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return StateUnknown, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return StateUnknown, err
	}
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return StateUnknown, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusMethodNotAllowed,
		resp.StatusCode == http.StatusNotImplemented, resp.StatusCode == http.StatusBadRequest:
		return StateUnsupported, nil
	case resp.StatusCode/100 != 2:
		return StateUnknown, fmt.Errorf("probe %s: status %d", path, resp.StatusCode)
	}
	if path == PathNative {
		var out SystemOneResponse
		if json.Unmarshal(data, &out) != nil || out.Questions["probe"].Choice == "" || len(out.Questions["probe"].Probabilities) == 0 {
			return StateUnsupported, nil
		}
		return StateSupported, nil
	}
	var out LogprobsResponse
	if json.Unmarshal(data, &out) != nil || !out.HasLogprobs() {
		return StateUnsupported, nil
	}
	return StateSupported, nil
}
