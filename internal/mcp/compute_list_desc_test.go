package mcp

import (
	"strings"
	"testing"
)

func TestComputeNodeListDescriptionMentionsObservedReports(t *testing.T) {
	s := &Server{}
	d := s.toolComputeNodeList().Description
	for _, w := range []string{"Observed peers with no node", "observed_peers", "unanswered_auto_created", "compute_node_add"} {
		if !strings.Contains(d, w) {
			t.Errorf("description missing %q: %s", w, d)
		}
	}
}
