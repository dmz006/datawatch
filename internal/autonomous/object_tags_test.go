package autonomous

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

// TestDeriveObjectTags pins the object-axes-only derivation against the
// operator's tags.placement block (section 12 of
// docs/plans/2026-11-06-automata-pages-redesign.md: "tag the objects too
// (Automata, Automations, tasks...) with project, arc and Automaton"): the
// same enabled axes, templates and custom axes as the placement tags apply,
// but the run axes (compute, peer, backend, lane, role) never yield a tag
// on an object, and an object with no project takes no project tag.
func TestDeriveObjectTags(t *testing.T) {
	// noProjectFacts is the fact set of an object outside a project:
	// everything to name but the project itself.
	noProjectFacts := ObjectTagFacts{
		Automation: "11111111",
		Automaton:  "22222222",
		Stage:      "build",
		Kind:       "task",
	}
	cases := []struct {
		name string
		fact ObjectTagFacts
		mut  func(pc *config.PlacementConfig)
		want []string
	}{
		{
			name: "all five facts take their axis tags under the defaults",
			fact: ObjectTagFacts{
				Project:    "datawatch",
				Automation: "11111111",
				Automaton:  "22222222",
				Stage:      "build",
				Kind:       "task",
			},
			mut:  nil,
			want: []string{"automation:11111111", "automaton:22222222", "kind:task", "project:datawatch", "stage:build"},
		},
		{
			name: "an object with no project takes no project tag, the rest still tag",
			fact: noProjectFacts,
			mut:  nil,
			want: []string{"automation:11111111", "automaton:22222222", "kind:task", "stage:build"},
		},
		{
			name: "the master switch off takes no tags",
			fact: ObjectTagFacts{Project: "datawatch", Automaton: "22222222"},
			mut:  func(pc *config.PlacementConfig) { pc.Enabled = false },
			want: nil,
		},
		{
			name: "a disabled axis takes no tag",
			fact: ObjectTagFacts{Project: "datawatch", Stage: "build"},
			mut:  func(pc *config.PlacementConfig) { pc.Stage.Enabled = false },
			want: []string{"project:datawatch"},
		},
		{
			name: "an operator template applies to the object axis",
			fact: ObjectTagFacts{Project: "datawatch"},
			mut: func(pc *config.PlacementConfig) {
				pc.Project.Template = "proj-{project}"
			},
			want: []string{"proj-datawatch"},
		},
		{
			name: "a custom axis reading an object fact tags under the object",
			fact: ObjectTagFacts{Automaton: "22222222"},
			mut: func(pc *config.PlacementConfig) {
				pc.Custom = append(pc.Custom, config.PlacementCustomAxis{
					Name:     "obj",
					Field:    "PRDID",
					Template: "obj-{value}",
					Enabled:  true,
				})
			},
			want: []string{"automaton:22222222", "obj-22222222"},
		},
		{
			name: "a run axis enabled everywhere still tags nothing on an object",
			fact: ObjectTagFacts{Project: "datawatch", Automaton: "22222222"},
			mut: func(pc *config.PlacementConfig) {
				// Every run axis on, plus a run custom axis on top.
				pc.Compute.Enabled, pc.Compute.Template = true, "run-{node}"
				pc.Peer.Enabled, pc.Peer.Template = true, "run-{peer}"
				pc.Backend.Enabled, pc.Backend.Template = true, "run-{backend}"
				pc.Lane.Enabled, pc.Lane.Template = true, "run-{lane}"
				pc.Role.Enabled, pc.Role.Template = true, "run-{role}"
				pc.Custom = append(pc.Custom, config.PlacementCustomAxis{
					Name: "run-node", Field: "Node",
					Template: "run-{value}", Enabled: true,
				})
			},
			want: []string{"automaton:22222222", "project:datawatch"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pc := config.DefaultPlacementConfig()
			if tc.mut != nil {
				tc.mut(&pc)
			}
			if got, want := DeriveObjectTags(tc.fact, pc), tc.want; !reflect.DeepEqual(got, want) {
				t.Fatalf("tags = %v, want %v", got, want)
			}
			for _, tag := range DeriveObjectTags(tc.fact, pc) {
				for _, runAxis := range []string{"compute:", "peer:", "backend:", "lane:", "role:"} {
					if strings.HasPrefix(tag, runAxis) {
						t.Fatalf("a run axis %q tagged an object: %v", runAxis, tag)
					}
				}
			}
		})
	}
}


// TestObjectTagsPerObject pins the per-object facts the Manager derives
// into each object's SystemTags field (still unwired for writes by this
// task): an Automaton takes its project, its Automation and itself; a
// task takes its Automaton's fact plus the stage its member slot carries;
// an Automation takes itself on the automation axis and the project of
// its first member.
func TestObjectTagsPerObject(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	// An Automaton inside an Automation whose slot carries a stage label,
	// in a project directory.
	prd, err := m.CreatePRD("tag the objects", "project/example", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "tag arc",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "build"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	if fresh.AutomationID != auto.ID {
		t.Fatalf("membership must stamp the PRD's AutomationID, got %q", fresh.AutomationID)
	}

	check := func(name string, got, want []string) {
		t.Helper()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s tags = %v, want %v", name, got, want)
		}
	}

	// Automaton: project + Automation + itself, no stage and no kind.
	check("Automaton", m.ObjectTagsForPRD(fresh), []string{
		"automation:" + auto.ID, "automaton:" + fresh.ID, "project:example",
	})

	// Task: the same fact plus the stage.
	task := &Task{ID: "t1", PRDID: fresh.ID}
	check("task", m.ObjectTagsForTask(task), []string{
		"automation:" + auto.ID, "automaton:" + fresh.ID, "project:example", "stage:build",
	})

	// Automation: itself + the project of its first member, no Automaton
	// and no stage of its own.
	check("Automation", m.ObjectTagsForAutomation(auto), []string{
		"automation:" + auto.ID, "project:example",
	})
}

// TestObjectTagsPerObject_NoProject pins the no-project case per object
// kind: an object that carries no project directory takes no project tag
// (the counts show it as "(none)"), and nothing else is invented.
func TestObjectTagsPerObject_NoProject(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	// Standalone: no project directory, no Automation either.
	prd, err := m.CreatePRD("outside a project", "", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	if got, want := m.ObjectTagsForPRD(prd), []string{"automaton:" + prd.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("standalone Automaton tags = %v, want %v", got, want)
	}
	task := &Task{ID: "t1", PRDID: prd.ID}
	if got, want := m.ObjectTagsForTask(task), []string{"automaton:" + prd.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("task on a standalone Automaton tags = %v, want %v", got, want)
	}
	// An Automation whose member has no project dir still names itself.
	auto, err := m.CreateAutomation(AutomationInput{
		Name:    "tag arc",
		Members: []AutomationMember{{PRDID: prd.ID, Stage: "build"}},
	}, "operator")
	if err != nil {
		t.Fatalf("CreateAutomation: %v", err)
	}
	if got, want := m.ObjectTagsForAutomation(auto), []string{"automation:" + auto.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Automation without a project tags = %v, want %v", got, want)
	}

	// Membership stamped the PRD into the Automation, but with no project
	// dir the Automation's fact still names only itself: the task's fact
	// likewise gains the Automation, still no project tag.
	fresh, ok := m.store.GetPRD(prd.ID)
	if !ok {
		t.Fatal("re-read PRD not found")
	}
	if got, want := m.ObjectTagsForPRD(fresh), []string{"automation:" + auto.ID, "automaton:" + fresh.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("member Automaton without a project tags = %v, want %v", got, want)
	}
}

// TestObjectTagsPerObject_OperatorConfig pins that the object derivations
// read the daemon's live tags.placement block: an operator template applies
// to the objects' project axis, and the run axes stay off the object even
// when the operator has enabled and templated them for the session.
func TestObjectTagsPerObject_OperatorConfig(t *testing.T) {
	m, err := NewManager(t.TempDir(), DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	pc := config.DefaultPlacementConfig()
	pc.Project.Template = "proj-{project}"
	pc.Role.Template = "run-{role}"
	pc.Role.Enabled = true
	m.SetPlacementTagSettings(func() config.PlacementConfig { return pc })

	prd, err := m.CreatePRD("tag under the operator block", "project/example", "pi", "", "")
	if err != nil {
		t.Fatalf("CreatePRD: %v", err)
	}
	got := m.ObjectTagsForPRD(prd)
	if !reflect.DeepEqual(got, []string{"automaton:" + prd.ID, "proj-example"}) {
		t.Fatalf("tags under the operator block = %v", got)
	}
}
