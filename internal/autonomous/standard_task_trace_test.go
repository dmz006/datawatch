package autonomous

import (
	"testing"

	"github.com/dmz006/datawatch/internal/packs"
	"github.com/dmz006/datawatch/internal/trace"
)

func TestStampBuiltinPackAttributesEachBuiltinTask(t *testing.T) {
	pack, err := packs.LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	defs := BuiltInTaskTemplates()
	if len(defs) != 6 {
		t.Fatalf("built-in standard tasks = %d, want 6", len(defs))
	}
	for _, d := range defs {
		ev := &trace.Event{}
		StampBuiltinPack(ev, pack.Version)
		if ev.PackName != "builtin" || ev.PackVersion != pack.Version {
			t.Errorf("task %q: got %q/%q", d.ID, ev.PackName, ev.PackVersion)
		}
	}
}

func TestStampBuiltinPackNilSafe(t *testing.T) {
	StampBuiltinPack(nil, "1")
}
