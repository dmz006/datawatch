package workerprofile

import (
	"reflect"
	"testing"
)

// fakeBackend stands in for a real launch backend: it declares which
// worker-profile fields it honours and records the outcomes the way the
// per-backend RecordApplied methods do.
type fakeBackend struct{ supported map[string]bool }

func (f fakeBackend) RecordApplied(p WorkerProfile) []FieldOutcome {
	return RecordApplied(p, f.supported)
}

var fullProfile = WorkerProfile{
	Name:    "full",
	MCP:     MCPSelection{Mode: MCPDatawatch},
	Tools:   ToolSelection{Mode: ToolsAll},
	Skills:  true,
	Context: ContextFull,
	Model:   "claude-opus-4-8",
	Effort:  "high",
}

func TestRecordAppliedFullSupport(t *testing.T) {
	b := fakeBackend{supported: map[string]bool{
		"mcp":     true,
		"tools":   true,
		"skills":  true,
		"context": true,
		"model":   true,
		"effort":  true,
	}}
	got := b.RecordApplied(fullProfile)
	want := []FieldOutcome{
		{Field: "mcp", Applied: true},
		{Field: "tools", Applied: true},
		{Field: "skills", Applied: true},
		{Field: "context", Applied: true},
		{Field: "model", Applied: true},
		{Field: "effort", Applied: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("full support:\ngot  %+v\nwant %+v", got, want)
	}
	for _, o := range got {
		if o.Reason != "" {
			t.Errorf("field %s: expected no reason, got %q", o.Field, o.Reason)
		}
	}
}

func TestRecordAppliedPartialSupport(t *testing.T) {
	// A backend that honours only the model pin: every other profile
	// field is dropped with the cannot-honour reason.
	b := fakeBackend{supported: map[string]bool{"model": true}}
	got := b.RecordApplied(fullProfile)
	if len(got) != len(FieldNames) {
		t.Fatalf("expected one outcome per field (%d), got %d", len(FieldNames), len(got))
	}
	for i, o := range got {
		if o.Field != FieldNames[i] {
			t.Fatalf("outcome %d: expected field %q in stable order, got %q", i, FieldNames[i], o.Field)
		}
		if o.Field == "model" {
			if !o.Applied || o.Reason != "" {
				t.Errorf("model: expected applied with no reason, got %+v", o)
			}
			continue
		}
		if o.Applied {
			t.Errorf("%s: expected not applied, got %+v", o.Field, o)
		}
		if o.Reason != CannotHonourReason {
			t.Errorf("%s: expected reason %q, got %q", o.Field, CannotHonourReason, o.Reason)
		}
	}
}

func TestRecordAppliedMissingAndFalseAreUnsupported(t *testing.T) {
	// "tools" present but false, "effort" missing from the map entirely:
	// both must be recorded as not applied with the cannot-honour reason.
	b := fakeBackend{supported: map[string]bool{
		"mcp":   true,
		"tools": false,
	}}
	got := b.RecordApplied(fullProfile)
	byField := make(map[string]FieldOutcome, len(got))
	for _, o := range got {
		byField[o.Field] = o
	}
	for _, field := range []string{"tools", "effort"} {
		o := byField[field]
		if o.Applied {
			t.Errorf("%s: expected not applied, got %+v", field, o)
		}
		if o.Reason != CannotHonourReason {
			t.Errorf("%s: expected reason %q, got %q", field, CannotHonourReason, o.Reason)
		}
	}
	if o := byField["mcp"]; !o.Applied || o.Reason != "" {
		t.Errorf("mcp: expected applied with no reason, got %+v", o)
	}
}

func TestRecordAppliedNilSupported(t *testing.T) {
	got := RecordApplied(fullProfile, nil)
	if len(got) != len(FieldNames) {
		t.Fatalf("expected %d outcomes, got %d", len(FieldNames), len(got))
	}
	for _, o := range got {
		if o.Applied {
			t.Errorf("%s: expected not applied with nil supported set, got %+v", o.Field, o)
		}
		if o.Reason != CannotHonourReason {
			t.Errorf("%s: expected reason %q, got %q", o.Field, CannotHonourReason, o.Reason)
		}
	}
}

func TestFormatAppliedStableSummary(t *testing.T) {
	outcomes := RecordApplied(fullProfile, map[string]bool{"mcp": true, "model": true})
	got := FormatApplied(outcomes)
	want := "mcp=applied " +
		"tools=unapplied: backend cannot honour this field " +
		"skills=unapplied: backend cannot honour this field " +
		"context=unapplied: backend cannot honour this field " +
		"model=applied " +
		"effort=unapplied: backend cannot honour this field"
	if got != want {
		t.Fatalf("summary mismatch:\ngot  %q\nwant %q", got, want)
	}

	// Shuffling the input order must not change the summary: it is stable
	// in FieldNames order, not input order.
	shuffled := append(append([]FieldOutcome{}, outcomes[len(outcomes)-1]), outcomes[:len(outcomes)-1]...)
	if got := FormatApplied(shuffled); got != want {
		t.Fatalf("summary not stable under reordering:\ngot  %q\nwant %q", got, want)
	}
}

func TestFormatAppliedAllSupported(t *testing.T) {
	outcomes := RecordApplied(fullProfile, map[string]bool{
		"mcp": true, "tools": true, "skills": true,
		"context": true, "model": true, "effort": true,
	})
	got := FormatApplied(outcomes)
	want := "mcp=applied tools=applied skills=applied context=applied model=applied effort=applied"
	if got != want {
		t.Fatalf("summary mismatch:\ngot  %q\nwant %q", got, want)
	}
}

func TestFormatAppliedEmpty(t *testing.T) {
	if got := FormatApplied(nil); got != "" {
		t.Fatalf("expected empty summary for no outcomes, got %q", got)
	}
}
