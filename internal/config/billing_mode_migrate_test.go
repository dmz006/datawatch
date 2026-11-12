package config

import (
	"testing"

	"github.com/dmz006/datawatch/internal/inference"
)

func newBillingTestRegistry(t *testing.T, llms ...*inference.LLM) *inference.Registry {
	t.Helper()
	r := inference.NewRegistry()
	for _, l := range llms {
		if err := r.Add(l); err != nil {
			t.Fatalf("add %s: %v", l.Name, err)
		}
	}
	return r
}

func billingOf(t *testing.T, r *inference.Registry, name string) string {
	t.Helper()
	l, err := r.Get(name)
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	return l.BillingMode
}

func TestMigrateBillingModesMixed(t *testing.T) {
	r := newBillingTestRegistry(t,
		&inference.LLM{Name: "claude", Kind: inference.KindClaudeCode},
		&inference.LLM{Name: "local", Kind: inference.KindOllama},
		&inference.LLM{Name: "other", Kind: inference.KindGemini},
		&inference.LLM{Name: "profiled", Kind: inference.KindOpenCode},
	)
	cfg := &Config{}
	cfg.Usage.Billing = map[string]BillingProfile{
		string(inference.KindOpenCode): {Mode: BillingModeBundledPlan},
	}
	if n := MigrateBillingModes(r, cfg); n != 4 {
		t.Fatalf("migrated %d, want 4", n)
	}
	want := map[string]string{
		"claude":   BillingModeAllowance,
		"local":    BillingModeLocal,
		"other":    BillingModeAPI,
		"profiled": BillingModeAllowance,
	}
	for name, mode := range want {
		if got := billingOf(t, r, name); got != mode {
			t.Errorf("%s: got %q, want %q", name, got, mode)
		}
	}
}

func TestMigrateBillingModesSecondRunNoop(t *testing.T) {
	r := newBillingTestRegistry(t,
		&inference.LLM{Name: "claude", Kind: inference.KindClaudeCode},
		&inference.LLM{Name: "other", Kind: inference.KindGemini},
	)
	if n := MigrateBillingModes(r, nil); n != 2 {
		t.Fatalf("first run migrated %d, want 2", n)
	}
	if n := MigrateBillingModes(r, nil); n != 0 {
		t.Fatalf("second run migrated %d, want 0", n)
	}
	if got := billingOf(t, r, "claude"); got != BillingModeAllowance {
		t.Errorf("claude: got %q", got)
	}
}

func TestMigrateBillingModesKeepsExplicit(t *testing.T) {
	r := newBillingTestRegistry(t,
		&inference.LLM{Name: "claude", Kind: inference.KindClaudeCode, BillingMode: BillingModeAPI},
		&inference.LLM{Name: "local", Kind: inference.KindOllama},
	)
	if n := MigrateBillingModes(r, nil); n != 1 {
		t.Fatalf("migrated %d, want 1", n)
	}
	if got := billingOf(t, r, "claude"); got != BillingModeAPI {
		t.Errorf("explicit mode overwritten: got %q", got)
	}
	if got := billingOf(t, r, "local"); got != BillingModeLocal {
		t.Errorf("local: got %q", got)
	}
}

func TestMigrateBillingModesNilRegistry(t *testing.T) {
	if n := MigrateBillingModes(nil, nil); n != 0 {
		t.Fatalf("got %d", n)
	}
}
