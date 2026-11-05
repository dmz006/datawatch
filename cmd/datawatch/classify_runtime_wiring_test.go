package main

import (
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/config"
)

func TestClassifyRuntimeProvider(t *testing.T) {
	d := &classifyDeps{dataDir: t.TempDir()}
	p := newClassifyRuntimeProvider(&config.Config{}, d)
	if _, err := p("n1", "", "m"); err == nil {
		t.Fatal("no compute registry must fail closed")
	}
	d.nodes = func(string) (*compute.Node, error) { return &compute.Node{Address: "gpu-node.example.test:11434"}, nil }
	if _, err := p("", "", "m"); err == nil {
		t.Fatal("unresolved node must fail closed")
	}
	rt, err := p("n1", "", "m")
	if err != nil || rt.Client == nil || rt.Calibration == nil || rt.Route.Node != "n1" || rt.Route.Model != "m" {
		t.Fatalf("runtime = %+v err=%v", rt, err)
	}
	if d.prober == nil {
		t.Fatal("prober not shared")
	}
	u, err := classifyBaseURL(d, "n1")
	if err != nil || u != "http://gpu-node.example.test:11434" {
		t.Fatalf("base url %q %v", u, err)
	}
	g := &ledgerGate{led: &d.ledger}
	if _, err := g.Acquire(t.Context(), "n1", "m"); err == nil {
		t.Fatal("gate without ledger must refuse")
	}
}
