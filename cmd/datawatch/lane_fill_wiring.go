package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dmz006/datawatch/internal/autonomous"
	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

// federationSessionLister lists the running sessions of this host and of every
// federation peer, through the same view the federation sessions endpoint
// serves.
type federationSessionLister struct {
	sessions func() server.FederationResponse
}

func (l federationSessionLister) RunningSessions(ctx context.Context) ([]autonomous.PoolSession, []autonomous.PoolHostFault) {
	if l.sessions == nil {
		return nil, []autonomous.PoolHostFault{{Host: "local", Err: errors.New("session list unavailable")}}
	}
	resp := l.sessions()
	var out []autonomous.PoolSession
	add := func(host string, list []*session.Session) {
		for _, s := range list {
			if s == nil || s.State != session.StateRunning {
				continue
			}
			out = append(out, autonomous.PoolSession{ID: s.FullID, Backend: s.BackendFamily, Host: host, Node: s.ComputeNodeRef})
		}
	}
	add("", resp.Primary)
	var faults []autonomous.PoolHostFault
	for name, list := range resp.Proxied {
		add(name, list)
	}
	for name, msg := range resp.Errors {
		faults = append(faults, autonomous.PoolHostFault{Host: name, Err: errors.New(msg)})
	}
	sort.Slice(faults, func(i, j int) bool { return faults[i].Host < faults[j].Host })
	return out, faults
}

// computePoolHosts lists the Ollama endpoints of this host and of the enabled
// compute nodes. An endpoint shared by several entries is listed once.
type computePoolHosts struct {
	reg       *compute.Registry
	localHost func() string
}

func (h computePoolHosts) PoolHosts(ctx context.Context) []autonomous.PoolHost {
	var out []autonomous.PoolHost
	seen := map[string]bool{}
	add := func(name, endpoint string) {
		endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if endpoint == "" || seen[endpoint] {
			return
		}
		seen[endpoint] = true
		out = append(out, autonomous.PoolHost{Name: name, Endpoint: endpoint})
	}
	if h.reg != nil {
		for _, n := range h.reg.List() {
			if n.Disabled || n.Kind != compute.KindOllama {
				continue
			}
			add(n.Name, n.Address)
		}
	}
	if h.localHost != nil {
		if host := strings.TrimSpace(h.localHost()); host != "" {
			name := "local"
			if u, err := url.Parse(host); err == nil && u.Hostname() != "" {
				name = u.Hostname()
			}
			add(name, host)
		}
	}
	return out
}

// newLaneFillEvidenceSource builds the pool evidence source lane fill plans
// with, over the real session list and the compute registry.
func newLaneFillEvidenceSource(mgr *autonomous.Manager, httpServer *server.HTTPServer, reg *compute.Registry, ollamaHost string) autonomous.PoolEvidenceSource {
	return autonomous.NewPoolEvidenceSource(
		federationSessionLister{sessions: httpServer.FederationSessions},
		computePoolHosts{reg: reg, localHost: func() string { return ollamaHost }},
		autonomous.NewHTTPModelFetcher(nil),
		autonomous.PoolEvidenceGPUBackend(func() string { return mgr.Config().LaneFill.EffectiveGPUBackend() }),
	)
}

// laneFillProposalHeadLimit caps how many bytes of a proposal file are read;
// the rule looks only at the first 200 lines.
const laneFillProposalHeadLimit = 64 * 1024

// repoProposalReader reads proposal files for the confirmed-proposal rule,
// under the Automaton base repository only. A reference that is absolute,
// holds a parent-directory element or resolves (through symbolic links) to a
// place outside the repository is reported as not existing, and nothing
// outside the repository is opened.
type repoProposalReader struct {
	root func() string
}

// ReadHead returns up to the first 64 KiB of the file at rel, a
// repository-relative path.
func (r repoProposalReader) ReadHead(rel string) ([]byte, error) {
	notFound := func(why string) error { return fmt.Errorf("proposal %q: %s: %w", rel, why, fs.ErrNotExist) }
	if rel == "" || strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") || filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return nil, notFound("absolute path")
	}
	for _, el := range strings.FieldsFunc(rel, func(c rune) bool { return c == '/' || c == '\\' }) {
		if el == ".." {
			return nil, notFound("parent-directory element")
		}
	}
	base := ""
	if r.root != nil {
		base = strings.TrimSpace(r.root())
	}
	if base == "" {
		return nil, errors.New("no Automaton base repository is configured")
	}
	realRoot, err := filepath.EvalSymlinks(base)
	if err != nil {
		return nil, err
	}
	realFile, err := filepath.EvalSymlinks(filepath.Join(realRoot, filepath.FromSlash(rel)))
	if err != nil {
		return nil, err // a missing file wraps fs.ErrNotExist
	}
	within, err := filepath.Rel(realRoot, realFile)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) || filepath.IsAbs(within) {
		return nil, notFound("resolves outside the repository")
	}
	f, err := os.Open(realFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, notFound("not a regular file")
	}
	return io.ReadAll(io.LimitReader(f, laneFillProposalHeadLimit))
}

// newLaneFillProposalReader builds the proposal reader over the Automaton base
// repository setting, read on every call so a config change applies at once.
func newLaneFillProposalReader(mgr *autonomous.Manager) autonomous.LaneFillProposalReader {
	return repoProposalReader{root: func() string {
		if mgr == nil {
			return ""
		}
		return mgr.Config().WorktreeBaseRepo
	}}
}
