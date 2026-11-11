package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/packs"
)

func TestStampBuiltinPackAttributesEachBuiltinTask(t *testing.T) {
	pack, err := packs.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if pack.Version == "" {
		t.Fatal("built-in pack has an empty version")
	}
	defs := BuiltInTaskTemplates()
	if len(defs) != 6 {
		t.Fatalf("built-in standard tasks = %d, want 6", len(defs))
	}
	seen := make(map[string]bool, len(defs))
	for _, d := range defs {
		if d.ID == "" {
			t.Fatalf("built-in task %q has an empty id", d.Title)
		}
		if seen[d.ID] {
			t.Fatalf("duplicate built-in task id %q", d.ID)
		}
		seen[d.ID] = true

		ev := BuiltinStandardTaskTraceEvent(d, pack.Version)
		if ev.TaskID != d.ID {
			t.Errorf("task %q: event TaskID = %q", d.ID, ev.TaskID)
		}
		if ev.Name != d.Title {
			t.Errorf("task %q: event Name = %q, want %q", d.ID, ev.Name, d.Title)
		}
		if ev.PackName != BuiltinPackName {
			t.Errorf("task %q: PackName = %q, want %q", d.ID, ev.PackName, BuiltinPackName)
		}
		if ev.PackVersion != pack.Version {
			t.Errorf("task %q: PackVersion = %q, want %q", d.ID, ev.PackVersion, pack.Version)
		}
	}
}

func TestStampBuiltinPackNilSafe(t *testing.T) {
	StampBuiltinPack(nil, "1")
}
