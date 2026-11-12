package classifier

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProbeStatus is one cached capability probe result, as shown to operators.
type ProbeStatus struct {
	Node      string    `json:"node"`
	Model     string    `json:"model"`
	Path      Path      `json:"path"`
	State     State     `json:"state"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Snapshot returns the unexpired cached probe results, ordered by node, model
// and path. An unknown result is never cached, so it never appears here.
func (p *Prober) Snapshot() []ProbeStatus {
	p.mu.Lock()
	now := p.now()
	out := make([]ProbeStatus, 0, len(p.cache))
	for k, e := range p.cache {
		if !now.Before(e.expires) {
			continue
		}
		out = append(out, ProbeStatus{Node: k.node, Model: k.model, Path: k.path, State: e.state, ExpiresAt: e.expires})
	}
	p.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		return a.Path < b.Path
	})
	return out
}

// List returns every stored calibration record, ordered by model, node and
// quantization. A missing directory is an empty list; an unreadable record is
// an error.
func (s FileStore) List() ([]*Record, error) {
	entries, err := os.ReadDir(s.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Record
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "calibration-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		rec, err := s.loadFile(filepath.Join(s.Dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Fingerprint, out[j].Fingerprint
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.Node != b.Node {
			return a.Node < b.Node
		}
		return a.Quantization < b.Quantization
	})
	return out, nil
}

// NormalizePublisher trims and lower-cases a publisher name.
func NormalizePublisher(p string) string { return strings.ToLower(strings.TrimSpace(p)) }

// PublisherAllowed reports whether publisher is on the allowlist. An empty
// allowlist or an empty publisher is never allowed: nothing is trusted until the
// operator adds it.
func PublisherAllowed(allowlist []string, publisher string) bool {
	p := NormalizePublisher(publisher)
	if p == "" {
		return false
	}
	for _, a := range allowlist {
		if NormalizePublisher(a) == p {
			return true
		}
	}
	return false
}

// AddPublisher returns the allowlist with publisher appended and whether it
// changed. The publisher is normalised; an empty name or a duplicate leaves the
// list as it was.
func AddPublisher(allowlist []string, publisher string) ([]string, bool) {
	p := NormalizePublisher(publisher)
	if p == "" || PublisherAllowed(allowlist, p) {
		return allowlist, false
	}
	return append(append([]string(nil), allowlist...), p), true
}
