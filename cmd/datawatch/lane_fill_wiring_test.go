package main

import (
	"context"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/server"
	"github.com/dmz006/datawatch/internal/session"
)

func TestLaneFillSessionListerKeepsRunningLocalAndRemote(t *testing.T) {
	l := federationSessionLister{sessions: func() server.FederationResponse {
		return server.FederationResponse{
			Primary: []*session.Session{
				{FullID: "h-1", State: session.StateRunning, BackendFamily: "pi-prompt"},
				{FullID: "h-2", State: session.StateComplete, BackendFamily: "pi-prompt"},
				nil,
			},
			Proxied: map[string][]*session.Session{
				"peer1": {{FullID: "p-1", State: session.StateRunning, BackendFamily: "opencode", ComputeNodeRef: "gpu-box"}},
			},
			Errors: map[string]string{"peer2": "transport: timeout"},
		}
	}}
	got, faults := l.RunningSessions(context.Background())
	if len(got) != 2 {
		t.Fatalf("sessions = %+v, want the two running ones", got)
	}
	var remote bool
	for _, s := range got {
		if s.ID == "p-1" {
			remote = s.Host == "peer1" && s.Node == "gpu-box" && s.Backend == "opencode"
		}
	}
	if !remote {
		t.Errorf("remote session lost its host or node: %+v", got)
	}
	if len(faults) != 1 || faults[0].Host != "peer2" {
		t.Errorf("faults = %+v", faults)
	}
}

func TestLaneFillPoolHostsListsOllamaNodesAndLocalOnce(t *testing.T) {
	reg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*compute.Node{
		{Name: "gpu-box", Kind: compute.KindOllama, Address: "http://10.0.0.5:11434/"},
		{Name: "off", Kind: compute.KindOllama, Address: "http://10.0.0.6:11434", Disabled: true},
		{Name: "api", Kind: compute.KindOpenAICompat, Address: "http://10.0.0.7:8000"},
	} {
		if err := reg.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	h := computePoolHosts{reg: reg, localHost: func() string { return "http://10.0.0.5:11434" }}
	got := h.PoolHosts(context.Background())
	if len(got) != 1 || got[0].Name != "gpu-box" || got[0].Endpoint != "http://10.0.0.5:11434" {
		t.Fatalf("hosts = %+v, want gpu-box once", got)
	}

	h.localHost = func() string { return "http://localhost:11434" }
	if got := h.PoolHosts(context.Background()); len(got) != 2 || got[1].Name != "localhost" {
		t.Fatalf("hosts = %+v, want gpu-box and the local host", got)
	}
}
