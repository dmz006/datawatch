package inference

import (
	"reflect"
	"testing"
)

func TestOrderedPoolNodesByPriority(t *testing.T) {
	llm := LLM{Name: "pool", Kind: "ollama", ComputeNodes: []string{"gamma", "alpha", "ghost", "beta", "alpha"}}
	prio := map[string]int{"alpha": 50, "beta": 80, "gamma": 50}
	got := OrderedPoolNodes(llm, func(n string) (int, bool) {
		p, ok := prio[n]
		return p, ok
	})
	want := []PoolNode{{"gamma", 50}, {"alpha", 50}, {"beta", 80}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v (list order kept, unknown and repeated nodes dropped)", got, want)
	}
}

func TestOrderedPoolNodesSingleNodeUnchanged(t *testing.T) {
	llm := LLM{Name: "solo", Kind: "ollama", ComputeNodes: []string{"alpha"}}
	got := OrderedPoolNodes(llm, func(string) (int, bool) { return 50, true })
	if want := []PoolNode{{"alpha", 50}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got := OrderedPoolNodes(llm, func(string) (int, bool) { return 0, false }); len(got) != 0 {
		t.Fatalf("unknown node kept: %+v", got)
	}
	if got := OrderedPoolNodes(llm, nil); len(got) != 0 {
		t.Fatalf("nil lookup returned %+v", got)
	}
	if got := OrderedPoolNodes(LLM{Name: "none"}, func(string) (int, bool) { return 50, true }); len(got) != 0 {
		t.Fatalf("entry with no nodes returned %+v", got)
	}
}
