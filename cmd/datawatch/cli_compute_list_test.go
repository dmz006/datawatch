package main

import (
	"strings"
	"testing"
)

func TestFormatComputeNodeListObservedSections(t *testing.T) {
	body := []byte(`{"nodes":[{"name":"n1","kind":"ollama","address":"a:1"}],"observed_peers":[{"name":"peer-a"}],"unanswered_auto_created":[{"name":"ghost","address":"b:2","kind":"ollama"}]}`)
	got := formatComputeNodeList(body)
	for _, w := range []string{"Observed peers with no node:", "datawatch compute node add peer-a --address <address you enter>", "Auto-created nodes that never answered:", "ghost"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "ghost (ollama, b:2)") {
		t.Errorf("sections must end the output:\n%s", got)
	}
	if got := formatComputeNodeList([]byte("not json")); !strings.Contains(got, "not json") {
		t.Errorf("fallback should echo body, got %q", got)
	}
}
