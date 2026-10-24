package server

import (
	"reflect"
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
	"github.com/dmz006/datawatch/internal/inference"
)

func TestResolvePoolsForBackendListsEveryNode(t *testing.T) {
	reg := inference.NewRegistry()
	if err := reg.Add(&inference.LLM{Name: "pool", Kind: "ollama", ComputeNodes: []string{"alpha", "beta"}}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Add(&inference.LLM{Name: "solo", Kind: "ollama", ComputeNodes: []string{"gamma"}}); err != nil {
		t.Fatal(err)
	}
	s := &Server{inferenceReg: reg}
	if got, want := s.resolvePoolsForBackend("pool"), []string{"llm:pool", "node:alpha", "node:beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("pool = %v, want %v", got, want)
	}
	if got, want := s.resolvePoolsForBackend("solo"), []string{"llm:solo", "node:gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("solo = %v, want %v", got, want)
	}

	creg, err := compute.NewRegistry(t.TempDir() + "/nodes.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*compute.Node{{Name: "alpha", Kind: compute.KindLocal}, {Name: "beta", Kind: compute.KindLocal, SchedulingPriority: 80}} {
		if err := creg.Add(n); err != nil {
			t.Fatal(err)
		}
	}
	s.computeReg = creg
	req, msg := s.admissionRequest(map[string][]string{"backend": {"pool"}})
	if msg != "" {
		t.Fatal(msg)
	}
	if want := []string{"host", "llm:pool"}; !reflect.DeepEqual(req.Pools, want) {
		t.Fatalf("pools = %v, want %v", req.Pools, want)
	}
	if len(req.NodeChoices) != 2 || req.NodeChoices[0].Name != "alpha" || req.NodeChoices[0].Priority != 50 || req.NodeChoices[1].Priority != 80 {
		t.Fatalf("choices = %+v", req.NodeChoices)
	}
	req, _ = s.admissionRequest(map[string][]string{"backend": {"solo"}})
	if want := []string{"host", "llm:solo", "node:gamma"}; !reflect.DeepEqual(req.Pools, want) || req.NodeChoices != nil {
		t.Fatalf("solo request = %+v", req)
	}
}
