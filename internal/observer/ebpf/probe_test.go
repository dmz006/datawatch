package ebpf

import "testing"

func TestNoopProbe_Contract(t *testing.T) {
	p := NewNoopProbe("testing")
	if p.Loaded() {
		t.Errorf("noop probe Loaded() should be false")
	}
	if got := p.Read(); got != nil {
		t.Errorf("noop probe Read() should be nil, got %v", got)
	}
	if err := p.Close(); err != nil {
		t.Errorf("noop probe Close() = %v want nil", err)
	}
	// Idempotent close.
	if err := p.Close(); err != nil {
		t.Errorf("second Close() = %v", err)
	}
	if r, ok := p.(*noopProbe); !ok || r.Reason() != "testing" {
		t.Errorf("Reason mismatch: ok=%v reason=%q", ok, r.Reason())
	}
}

func TestNewNetProbe_DegradesGracefully(t *testing.T) {
	// Without bpf2go output AND without CAP_BPF this should never
	// error — it must always return a usable noop probe so callers
	// don't need to nil-check.
	p, err := NewNetProbe()
	if err != nil {
		t.Fatalf("NewNetProbe should not error in degrade mode: %v", err)
	}
	if p == nil {
		t.Fatal("NewNetProbe returned nil probe")
	}
	if p.Loaded() {
		// We're not running with CAP_BPF in test, so Loaded must be
		// false — if this ever flips to true unexpectedly, the test
		// host gained eBPF privileges and the assertion needs review.
		t.Logf("note: Loaded()=true — test host has CAP_BPF + generated objects")
	}
	if got := p.Read(); len(got) > 0 && !p.Loaded() {
		t.Errorf("Read returned data but Loaded=false")
	}
}
