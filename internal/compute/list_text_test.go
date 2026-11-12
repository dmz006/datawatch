package compute

import (
	"strings"
	"testing"
)

func TestFormatListText(t *testing.T) {
	full := `{"nodes":[{"name":"n1","kind":"ollama","address":"node-one.example:11434"}],
"observed_peers":[{"name":"peer-a","shape":"gpu"}],
"unanswered_auto_created":[{"name":"ghost","address":"ghost-host.example:11434","kind":"ollama"}]}`
	empty := `{"nodes":[],"observed_peers":[],"unanswered_auto_created":[]}`
	cases := []struct {
		name, body, addLine string
		want, notWant       []string
		wantErr             bool
	}{
		{"cli full", full, CLIAddLine, []string{
			"n1 (ollama, node-one.example:11434)",
			"Observed peers with no node:",
			"peer-a [gpu] - Add as a compute node: datawatch compute node add peer-a --address <address you enter>",
			"Auto-created nodes that never answered:",
			"ghost (ollama, ghost-host.example:11434)"}, nil, false},
		{"comm full", full, CommAddLine, []string{"compute node add peer-a kind=<kind> address=<address you enter>"}, []string{"--address"}, false},
		{"empty", empty, CLIAddLine, []string{"Observed peers with no node:\n  (none)", "never answered:\n  (none)"}, nil, false},
		{"old body without keys", `{"nodes":[]}`, CLIAddLine, []string{"Observed peers with no node:", "never answered:"}, nil, false},
		{"bad json", `nope`, CLIAddLine, nil, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := FormatListText([]byte(c.body), c.addLine)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range c.notWant {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in:\n%s", w, got)
				}
			}
		})
	}
}
