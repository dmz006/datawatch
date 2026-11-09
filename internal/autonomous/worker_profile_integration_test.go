package autonomous

import (
	"os"
	"strings"
	"testing"

	"github.com/dmz006/datawatch/internal/channel"
	"github.com/dmz006/datawatch/internal/workerprofile"
)

// Integration test for the worker profile launch path. It wires resolution,
// the MCP file writer, the starting context builder, an attempt record and
// RecordApplied together using fake types only. No process is started and the
// only files touched are inside t.TempDir().

// fakeAttempt stands in for the attempt record a launch writes.
type fakeAttempt struct {
	Decision workerprofile.Decision
	Profile  workerprofile.WorkerProfile
	MCPPath  string
	Context  string
	Applied  []workerprofile.FieldOutcome
}

// fakeBackend stands in for a backend; it honours only the fields in Supports.
type fakeBackend struct{ Supports map[string]bool }

// fakeLaunch runs the launch pipeline against a fake backend.
func fakeLaunch(t *testing.T, in workerprofile.ResolveInput, be fakeBackend, taskSpec string) fakeAttempt {
	t.Helper()
	p, d := workerprofile.Resolve(in)
	servers := map[string]channel.MCPServerSpec{
		channel.ProjectMCPEntryName: {Command: "fake-datawatch", Args: []string{"mcp"}},
		"other":                     {Command: "fake-other"},
	}
	path, err := channel.WriteWorkerMCPConfig(t.TempDir(), p.MCP, servers)
	if err != nil {
		t.Fatalf("WriteWorkerMCPConfig: %v", err)
	}
	ctx := BuildWorkerStartingContext(StartingContextInput{
		Role:        in.Role,
		Profile:     p,
		TaskSpec:    taskSpec,
		Constraints: []string{"stay in scope"},
		RulesDigest: "digest text",
		RulesFull:   "full rules text",
	})
	return fakeAttempt{
		Decision: d,
		Profile:  p,
		MCPPath:  path,
		Context:  ctx,
		Applied:  workerprofile.RecordApplied(p, be.Supports),
	}
}

func allFieldsBackend() fakeBackend {
	m := map[string]bool{}
	for _, f := range workerprofile.FieldNames {
		m[f] = true
	}
	return fakeBackend{Supports: m}
}

func TestWorkerProfileIntegration_OverrideOrder(t *testing.T) {
	// Each case names every layer at or below the winner, so the case proves
	// the winner beats all of the less specific layers.
	cases := []struct {
		name      string
		in        workerprofile.ResolveInput
		wantName  string
		wantLayer workerprofile.Layer
	}{
		{"task beats automaton", workerprofile.ResolveInput{Role: "worker", Kind: "note",
			TaskName: "full", AutomatonName: "minimal", ProjectName: "digest", GlobalName: "datawatch-tools"},
			"full", workerprofile.LayerTask},
		{"automaton beats project", workerprofile.ResolveInput{Role: "worker", Kind: "note",
			AutomatonName: "full", ProjectName: "minimal", GlobalName: "digest"},
			"full", workerprofile.LayerAutomaton},
		{"project beats global", workerprofile.ResolveInput{Role: "worker", Kind: "note",
			ProjectName: "full", GlobalName: "minimal"},
			"full", workerprofile.LayerProject},
		{"global beats kind default", workerprofile.ResolveInput{Role: "worker", Kind: "note",
			GlobalName: "full"},
			"full", workerprofile.LayerGlobal},
		{"kind default beats role default", workerprofile.ResolveInput{Role: "planner", Kind: "note"},
			"minimal", workerprofile.LayerKind},
		{"role default alone", workerprofile.ResolveInput{Role: "planner"},
			"full", workerprofile.LayerRole},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := fakeLaunch(t, tc.in, allFieldsBackend(), "do the thing")
			if a.Profile.Name != tc.wantName {
				t.Errorf("profile = %q, want %q", a.Profile.Name, tc.wantName)
			}
			if a.Decision.Layer != string(tc.wantLayer) {
				t.Errorf("layer = %q, want %q", a.Decision.Layer, tc.wantLayer)
			}
			if a.Decision.Applied != tc.wantName {
				t.Errorf("decision applied = %q, want %q", a.Decision.Applied, tc.wantName)
			}
			if !strings.Contains(a.Context, "profile=\""+tc.wantName+"\"") {
				t.Errorf("starting context does not carry profile %q", tc.wantName)
			}
		})
	}
}

func TestWorkerProfileIntegration_UnknownNameFallsBackToRoleDefault(t *testing.T) {
	in := workerprofile.ResolveInput{
		Role:          "worker",
		Kind:          "note", // kind default would be minimal; role default must win
		TaskName:      "no-such-profile",
		AutomatonName: "full", // a later layer must not rescue an unknown name
	}
	a := fakeLaunch(t, in, allFieldsBackend(), "task")
	if a.Profile.Name != "digest" {
		t.Errorf("profile = %q, want role default digest", a.Profile.Name)
	}
	d := a.Decision
	if d.Requested != "no-such-profile" {
		t.Errorf("decision requested = %q, want the unknown name", d.Requested)
	}
	if d.Applied != "digest" {
		t.Errorf("decision applied = %q, want digest", d.Applied)
	}
	if d.Layer != string(workerprofile.LayerTask) {
		t.Errorf("decision layer = %q, want %q", d.Layer, workerprofile.LayerTask)
	}
	if !strings.Contains(d.Reason, "unknown profile") {
		t.Errorf("decision reason = %q, want it to mention an unknown profile", d.Reason)
	}
}

func TestWorkerProfileIntegration_StartingContextStableBeforeTask(t *testing.T) {
	in := workerprofile.ResolveInput{Role: "worker", TaskName: "full"}
	a := fakeLaunch(t, in, allFieldsBackend(), "first task spec")
	b := fakeLaunch(t, in, allFieldsBackend(), "a completely different task spec")

	prefix := func(s string) string {
		i := strings.Index(s, "Task\n")
		if i < 0 {
			t.Fatalf("context has no Task section: %q", s)
		}
		return s[:i]
	}
	pa, pb := prefix(a.Context), prefix(b.Context)
	if pa != pb {
		t.Errorf("context before Task differs:\n%q\n%q", pa, pb)
	}
	if !strings.Contains(pa, "Rules digest") || !strings.Contains(pa, "Rules\n") {
		t.Errorf("full profile context missing rules sections: %q", pa)
	}
	if a.Context == b.Context {
		t.Error("contexts should differ after the Task section")
	}
}

func TestWorkerProfileIntegration_UnsupportedFieldRecordedNotApplied(t *testing.T) {
	be := fakeBackend{Supports: map[string]bool{"model": true, "effort": true}}
	a := fakeLaunch(t, workerprofile.ResolveInput{Role: "worker"}, be, "task")

	if len(a.Applied) != len(workerprofile.FieldNames) {
		t.Fatalf("got %d outcomes, want %d", len(a.Applied), len(workerprofile.FieldNames))
	}
	for _, o := range a.Applied {
		wantApplied := be.Supports[o.Field]
		if o.Applied != wantApplied {
			t.Errorf("field %s applied = %v, want %v", o.Field, o.Applied, wantApplied)
		}
		if !wantApplied && o.Reason != workerprofile.CannotHonourReason {
			t.Errorf("field %s reason = %q, want %q", o.Field, o.Reason, workerprofile.CannotHonourReason)
		}
		if wantApplied && o.Reason != "" {
			t.Errorf("field %s applied but has reason %q", o.Field, o.Reason)
		}
	}
	summary := workerprofile.FormatApplied(a.Applied)
	for _, f := range []string{"mcp", "tools", "skills", "context"} {
		if !strings.Contains(summary, f+"=unapplied") {
			t.Errorf("summary %q missing unapplied %s", summary, f)
		}
	}
}

func TestWorkerProfileIntegration_MCPFileMatchesProfile(t *testing.T) {
	a := fakeLaunch(t, workerprofile.ResolveInput{Role: "judge"}, allFieldsBackend(), "task")
	raw, err := os.ReadFile(a.MCPPath)
	if err != nil {
		t.Fatalf("read worker mcp file: %v", err)
	}
	// judge resolves to minimal, which carries no MCP servers.
	if strings.Contains(string(raw), "fake-") {
		t.Errorf("minimal profile wrote servers: %s", raw)
	}
}
