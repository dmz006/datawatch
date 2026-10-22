package router

import "testing"

func TestParseKVPairs_PinFields(t *testing.T) {
	got := parseKVPairs("kind=ollama node_pinned_model=qwen3.8:27b allow_model_swap=true")
	if got["node_pinned_model"] != "qwen3.8:27b" {
		t.Fatalf("node_pinned_model = %#v", got["node_pinned_model"])
	}
	if got["allow_model_swap"] != true {
		t.Fatalf("allow_model_swap = %#v, want bool true", got["allow_model_swap"])
	}
	off := parseKVPairs("allow_model_swap=no")
	if off["allow_model_swap"] != false {
		t.Fatalf("allow_model_swap=no = %#v, want bool false", off["allow_model_swap"])
	}
}
