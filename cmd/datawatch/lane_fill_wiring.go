package main

import (
	"context"
	"errors"
	"net/url"
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
