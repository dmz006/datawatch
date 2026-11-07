package autonomous

import (
	"encoding/json"
	"testing"

	"github.com/dmz006/datawatch/internal/config"
)

func TestMemoryLimit_UnsetStaysEmpty(t *testing.T) {
	var p config.ProjectPolicy
	var c config.AutonomousConfig
	if ProjectMemoryLimit(&p) != "" || ProjectMemoryLimit(nil) != "" || KindMemoryLimit(c.MemoryLimitByKind, "bug") != "" {
		t.Fatal("unset must read as empty")
	}
	b, _ := json.Marshal(p)
	if string(b) != "{}" {
		t.Fatalf("unset field must be omitted, got %s", b)
	}
	if err := p.Validate(); err != nil {
		t.Fatalf("unset must be valid: %v", err)
	}
	if err := config.ValidateMemoryLimitByKind(c.MemoryLimitByKind); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryLimit_NumberAndNoneRoundTrip(t *testing.T) {
	for _, v := range []string{"4096", "none"} {
		p := config.ProjectPolicy{MemoryLimit: &v}
		b, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		var back config.ProjectPolicy
		if err := json.Unmarshal(b, &back); err != nil {
			t.Fatal(err)
		}
		if got := ProjectMemoryLimit(&back); got != v {
			t.Errorf("project round trip %q: got %q", v, got)
		}
		if err := back.Validate(); err != nil {
			t.Errorf("%q must validate: %v", v, err)
		}

		c := config.AutonomousConfig{MemoryLimitByKind: map[string]string{"smoke": v}}
		b, _ = json.Marshal(c)
		var cb config.AutonomousConfig
		if err := json.Unmarshal(b, &cb); err != nil {
			t.Fatal(err)
		}
		if got := KindMemoryLimit(cb.MemoryLimitByKind, "smoke"); got != v {
			t.Errorf("kind round trip %q: got %q", v, got)
		}
		if err := config.ValidateMemoryLimitByKind(cb.MemoryLimitByKind); err != nil {
			t.Errorf("%q must validate: %v", v, err)
		}
	}
}

func TestMemoryLimit_InvalidRejected(t *testing.T) {
	for _, v := range []string{"0", "-5", "lots", "1.5"} {
		v := v
		p := config.ProjectPolicy{MemoryLimit: &v}
		if p.Validate() == nil {
			t.Errorf("project %q must be rejected", v)
		}
		if config.ValidateMemoryLimitByKind(map[string]string{"bug": v}) == nil {
			t.Errorf("kind %q must be rejected", v)
		}
	}
}
