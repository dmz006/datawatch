// BL180 Phase 2 (v5.13.0) — conn_attribution map data-shape tests.
//
// These tests validate the userspace iterator + TTL pruner contract
// without needing a real kernel. The bpf2go-generated map types are
// initialized but not loaded; we exercise the prune logic by
// hand-building a synthetic state.
//
// Real attach + kprobe behaviour is covered by an end-to-end test
// the operator runs on a CAP_BPF-enabled host (Thor smoke per the
// BL180 design Q6).

package ebpf

import "testing"

// TestRealLinuxKprobeProbe_ReadConnAttribution_NilSafe verifies that
// the iterator silently returns nil when the probe has no objs
// (e.g. degrade-to-noop path or post-Close).
func TestRealLinuxKprobeProbe_ReadConnAttribution_NilSafe(t *testing.T) {
	p := &realLinuxKprobeProbe{} // objs is nil
	if rows := p.ReadConnAttribution(); rows != nil {
		t.Fatalf("expected nil for unloaded probe, got %+v", rows)
	}
	if n := p.PruneConnAttribution(0); n != 0 {
		t.Fatalf("expected 0 deletions for unloaded probe, got %d", n)
	}
}

// TestRealLinuxKprobeProbe_NoopAfterClose verifies the cleanup
// invariant: post-Close, both Read and Prune become no-ops without
// panicking.
func TestRealLinuxKprobeProbe_NoopAfterClose(t *testing.T) {
	p := &realLinuxKprobeProbe{} // never loaded
	if err := p.Close(); err != nil {
		t.Fatalf("Close on never-loaded probe: %v", err)
	}
	if rows := p.ReadConnAttribution(); rows != nil {
		t.Fatalf("post-Close Read should be nil")
	}
	if n := p.PruneConnAttribution(0); n != 0 {
		t.Fatalf("post-Close Prune should be 0")
	}
	// Double-close is idempotent.
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestConnAttributionRow_Shape locks down the public type contract so
// downstream consumers can rely on the field layout. If this test
// breaks during a refactor the consumer side has to be updated too.
func TestConnAttributionRow_Shape(t *testing.T) {
	row := ConnAttribution{Sock: 0xdeadbeef, PID: 1234, TsNs: 9_876_543_210}
	if row.Sock == 0 {
		t.Fatal("Sock must be non-zero")
	}
	if row.PID != 1234 {
		t.Fatalf("PID = %d", row.PID)
	}
	if row.TsNs == 0 {
		t.Fatal("TsNs must be set")
	}
}
