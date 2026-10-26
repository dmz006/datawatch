package main

import (
	"path/filepath"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/session"
)

func TestSessionComputeNodePrefersExplicitRefOverEndpoint(t *testing.T) {
	reg, err := compute.NewRegistry(filepath.Join(t.TempDir(), "nodes.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*compute.Node{
		{Name: "gpu-a", Kind: compute.KindOllama, Address: "http://10.0.0.1:11434"},
		{Name: "gpu-b", Kind: compute.KindOllama, Address: "http://10.0.0.2:11434"},
	} {
		if err := reg.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	sessions := map[string]*session.Session{
		"ref":  {ComputeNodeRef: "gpu-a", NodeAddress: "http://10.0.0.2:11434"},
		"addr": {NodeAddress: "HTTP://10.0.0.2:11434/"},
		"none": {},
	}
	fn := sessionComputeNode(func(id string) (*session.Session, bool) { s, ok := sessions[id]; return s, ok }, func() *compute.Registry { return reg })

	if got := fn("ref"); got != "gpu-a" {
		t.Fatalf("explicit ref wins, got %q", got)
	}
	if got := fn("addr"); got != "gpu-b" {
		t.Fatalf("endpoint fallback should find gpu-b, got %q", got)
	}
	if got := fn("none"); got != "" {
		t.Fatalf("no placement should give no node, got %q", got)
	}
	if got := fn("missing"); got != "" {
		t.Fatalf("unknown session should give no node, got %q", got)
	}
}
