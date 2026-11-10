package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func recordIntPtr(n int) *int { return &n }

func TestProjectConfigEntry_Validate(t *testing.T) {
	tests := []struct {
		name    string
		entry   ProjectConfigEntry
		wantErr string // substring; empty means valid
	}{
		{name: "empty entry is valid", entry: ProjectConfigEntry{Dir: "/x"}},
		{
			name: "all fields valid",
			entry: ProjectConfigEntry{
				Arcs:                []ProjectArc{{AutomationID: "a1", State: ArcStateActive}, {AutomationID: "a2", State: ""}},
				WeeklyShare:         recordIntPtr(100),
				BranchRetentionDays: recordIntPtr(0),
				MajorTrigger:        MajorTriggerMilestone,
			},
		},
		{name: "weekly share zero valid", entry: ProjectConfigEntry{WeeklyShare: recordIntPtr(0)}},
		{name: "unknown arc state", entry: ProjectConfigEntry{Arcs: []ProjectArc{{AutomationID: "a", State: "blocked"}}}, wantErr: "unknown state"},
		{name: "empty arc id", entry: ProjectConfigEntry{Arcs: []ProjectArc{{State: ArcStateDone}}}, wantErr: "automation_id is required"},
		{
			name:    "duplicate arc id",
			entry:   ProjectConfigEntry{Arcs: []ProjectArc{{AutomationID: "a"}, {AutomationID: "a"}}},
			wantErr: "duplicate automation_id",
		},
		{name: "share above 100", entry: ProjectConfigEntry{WeeklyShare: recordIntPtr(101)}, wantErr: "outside 0-100"},
		{name: "share below 0", entry: ProjectConfigEntry{WeeklyShare: recordIntPtr(-1)}, wantErr: "outside 0-100"},
		{name: "negative retention", entry: ProjectConfigEntry{BranchRetentionDays: recordIntPtr(-3)}, wantErr: "negative"},
		{name: "unknown major trigger", entry: ProjectConfigEntry{MajorTrigger: "weekly"}, wantErr: "unknown major_trigger"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.entry.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Validate() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestProjectConfigEntry_OrderedArcs(t *testing.T) {
	e := ProjectConfigEntry{Arcs: []ProjectArc{
		{AutomationID: "late", Priority: 5},
		{AutomationID: "tie-first", Priority: 1},
		{AutomationID: "first", Priority: 0},
		{AutomationID: "tie-second", Priority: 1},
	}}
	got := e.OrderedArcs()
	want := []string{"first", "tie-first", "tie-second", "late"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, id := range want {
		if got[i].AutomationID != id {
			t.Errorf("index %d = %q, want %q", i, got[i].AutomationID, id)
		}
	}
	// The declared slice must not be reordered.
	if e.Arcs[0].AutomationID != "late" {
		t.Errorf("OrderedArcs mutated the declared arcs")
	}
}

func TestProjectConfigEntry_OrderedArcsEmpty(t *testing.T) {
	if got := (ProjectConfigEntry{}).OrderedArcs(); len(got) != 0 {
		t.Errorf("OrderedArcs on empty entry = %v, want empty", got)
	}
}

func TestProjectConfigEntry_YAMLRoundTrip(t *testing.T) {
	in := ProjectConfigEntry{
		Dir:           "/srv/acme",
		Owner:         "platform",
		Repository:    "github.com/acme/app",
		DefaultBranch: "main",
		Arcs: []ProjectArc{
			{AutomationID: "auto-1", Priority: 2, State: ArcStateActive},
			{AutomationID: "auto-2", Priority: 1, State: ArcStatePlanned},
		},
		Target:              &ProjectTarget{Name: "v2 launch", DefinitionOfDone: "all arcs done"},
		LaneDefaults:        &ProjectLaneDefaults{Lane: "claude", Backend: "claude-code", Model: "sonnet"},
		WeeklyShare:         recordIntPtr(40),
		BranchRetentionDays: recordIntPtr(14),
		MajorTrigger:        MajorTriggerManual,
	}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out ProjectConfigEntry
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, data)
	}
	if out.Owner != in.Owner || out.Repository != in.Repository || out.DefaultBranch != in.DefaultBranch {
		t.Errorf("scalar fields changed: %+v", out)
	}
	if len(out.Arcs) != 2 || out.Arcs[0] != in.Arcs[0] || out.Arcs[1] != in.Arcs[1] {
		t.Errorf("arcs changed: %+v", out.Arcs)
	}
	if out.Target == nil || *out.Target != *in.Target {
		t.Errorf("target changed: %+v", out.Target)
	}
	if out.LaneDefaults == nil || *out.LaneDefaults != *in.LaneDefaults {
		t.Errorf("lane_defaults changed: %+v", out.LaneDefaults)
	}
	if out.WeeklyShare == nil || *out.WeeklyShare != 40 {
		t.Errorf("weekly_share changed: %v", out.WeeklyShare)
	}
	if out.BranchRetentionDays == nil || *out.BranchRetentionDays != 14 {
		t.Errorf("branch_retention_days changed: %v", out.BranchRetentionDays)
	}
	if out.MajorTrigger != MajorTriggerManual {
		t.Errorf("major_trigger = %q", out.MajorTrigger)
	}
	if err := out.Validate(); err != nil {
		t.Errorf("round-tripped entry invalid: %v", err)
	}
}

func TestProjectConfigEntry_PlainEntryUnchanged(t *testing.T) {
	in := ProjectConfigEntry{Dir: "/srv/plain", DefaultBackend: "claude-code", Description: "plain"}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"owner", "repository", "default_branch", "arcs", "target", "lane_defaults", "weekly_share", "branch_retention_days", "major_trigger"} {
		if strings.Contains(string(data), key+":") {
			t.Errorf("plain entry emitted %q:\n%s", key, data)
		}
	}
	var out ProjectConfigEntry
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Dir != in.Dir || out.DefaultBackend != in.DefaultBackend || out.Description != in.Description {
		t.Errorf("plain entry changed: %+v", out)
	}
	if out.Arcs != nil || out.Target != nil || out.LaneDefaults != nil || out.WeeklyShare != nil {
		t.Errorf("record fields materialised on plain entry: %+v", out)
	}
}
