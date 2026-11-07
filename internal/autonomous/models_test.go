package autonomous

import (
	"encoding/json"
	"testing"
)

func TestMemoryLimit_UnsetStaysEmpty(t *testing.T) {
	var p ProjectRecord
	var k TaskKindPolicy
	if p.MemoryLimit != "" || k.MemoryLimit != "" {
		t.Fatal("zero value must be empty")
	}
	b, _ := json.Marshal(p)
	if string(b) != "{}" {
		t.Fatalf("unset field must be omitted, got %s", b)
	}
	var back ProjectRecord
	if err := json.Unmarshal(b, &back); err != nil || back.MemoryLimit != "" {
		t.Fatalf("round trip: %v %q", err, back.MemoryLimit)
	}
	if p.Validate() != nil || k.Validate() != nil {
		t.Fatal("empty must be valid")
	}
}

func TestMemoryLimit_RoundTrip(t *testing.T) {
	for _, v := range []string{"4096", MemoryLimitNone} {
		b, _ := json.Marshal(ProjectRecord{Name: "a", MemoryLimit: v})
		var p ProjectRecord
		if err := json.Unmarshal(b, &p); err != nil || p.MemoryLimit != v || p.Validate() != nil {
			t.Fatalf("project %q: %v %+v", v, err, p)
		}
		b, _ = json.Marshal(TaskKindPolicy{Kind: TaskKindSmoke, MemoryLimit: v})
		var k TaskKindPolicy
		if err := json.Unmarshal(b, &k); err != nil || k.MemoryLimit != v || k.Validate() != nil {
			t.Fatalf("kind %q: %v %+v", v, err, k)
		}
	}
}

func TestMemoryLimit_InvalidRejected(t *testing.T) {
	for _, v := range []string{"0", "-5", "lots", "1.5", "10GB"} {
		if ValidateMemoryLimit(v) == nil {
			t.Errorf("%q must be rejected", v)
		}
		if (ProjectRecord{MemoryLimit: v}).Validate() == nil || (TaskKindPolicy{MemoryLimit: v}).Validate() == nil {
			t.Errorf("%q must be rejected on records", v)
		}
	}
}
