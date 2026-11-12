package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/session"
)

func TestSessionPlacement(t *testing.T) {
	automations := func(prdID string) string {
		if prdID == "prd-1" {
			return "auto-1"
		}
		return ""
	}
	cases := []struct {
		name         string
		s            *session.Session
		localNode    string
		automationOf func(string) string
		want         Placement
	}{
		{
			name:      "local session with no compute node takes local node",
			s:         &session.Session{BackendFamily: "claude"},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Backend: "claude", Lane: string(LaneTypeClaude)},
		},
		{
			name:      "session compute node ref is kept over local node",
			s:         &session.Session{ComputeNodeRef: "gpu-1", BackendFamily: "pi-prompt"},
			localNode: "local-box",
			want:      Placement{Node: "gpu-1", Backend: "pi-prompt", Lane: string(LaneTypeGPU)},
		},
		{
			name:         "no PRDID leaves automaton and automation empty",
			s:            &session.Session{BackendFamily: "claude"},
			localNode:    "local-box",
			automationOf: automations,
			want:         Placement{Node: "local-box", Backend: "claude", Lane: string(LaneTypeClaude)},
		},
		{
			name:         "PRDID gets the automaton and its automation",
			s:            &session.Session{PRDID: "prd-1", BackendFamily: "claude"},
			localNode:    "local-box",
			automationOf: automations,
			want: Placement{
				Node: "local-box", Backend: "claude", Lane: string(LaneTypeClaude),
				Automaton: "prd-1", Automation: "auto-1",
			},
		},
		{
			name:         "PRDID without a known automation keeps automation empty",
			s:            &session.Session{PRDID: "prd-2"},
			localNode:    "local-box",
			automationOf: automations,
			want:         Placement{Node: "local-box", Automaton: "prd-2"},
		},
		{
			name:      "nil automationOf leaves automation empty",
			s:         &session.Session{PRDID: "prd-1"},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Automaton: "prd-1"},
		},
		{
			name:      "empty launch stage gives empty stage",
			s:         &session.Session{LaunchKind: "task", LaunchStage: ""},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Kind: "task"},
		},
		{
			name:      "launch stage is carried through",
			s:         &session.Session{LaunchStage: "review"},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Stage: "review"},
		},
		{
			name:      "no launch role gives empty role",
			s:         &session.Session{LaunchKind: "decompose"},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Kind: "decompose"},
		},
		{
			name:      "launch role is carried through",
			s:         &session.Session{LaunchRole: "verifier"},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Role: "verifier"},
		},
		{
			name:      "project dir sets project to its base name",
			s:         &session.Session{ProjectDir: "/work/projects/datawatch"},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Project: "datawatch"},
		},
		{
			name:      "unknown backend family gives empty lane",
			s:         &session.Session{BackendFamily: "mystery"},
			localNode: "local-box",
			want:      Placement{Node: "local-box", Backend: "mystery"},
		},
		{
			name:      "empty inputs produce an empty placement",
			s:         &session.Session{},
			localNode: "",
			want:      Placement{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SessionPlacement(tc.s, tc.localNode, tc.automationOf)
			if got != tc.want {
				t.Errorf("SessionPlacement() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSessionPlacementNilSession(t *testing.T) {
	if got := SessionPlacement(nil, "local-box", nil); got != (Placement{}) {
		t.Errorf("SessionPlacement(nil) = %+v, want empty", got)
	}
}
