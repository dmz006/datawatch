package fingerprint

import (
	"testing"

	"github.com/dmz006/datawatch/internal/compute"
)

// sameNodeInfo models one physical node's HostInfo envelope: two GPU
// UUIDs and a disk-persisted instance id.
func sameNodeInfo() map[string]any {
	return map[string]any{
		"hostname":         "gpu-box",
		"os":               "linux",
		"arch":             "arm64",
		"gpu_uuids":        []string{"GPU-0f88de7f-9c8b-0d4a-c6e0-ba9e5c1d0a2f", "GPU-1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"},
		"node_instance_id": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",
	}
}

func localResolver(info map[string]any) *IdentityResolver {
	return &IdentityResolver{Local: func() map[string]any { return info }}
}

// One physical node reached through a LAN address and a tailnet
// address: two registry entries with different Name and Address but
// the same host info must give an identical value — and therefore an
// identical Hash under one peer-pair key.
func TestResolveSameNodeTwoAddresses(t *testing.T) {
	r := localResolver(sameNodeInfo())
	lan := compute.Node{Name: "gpu-lan", Kind: compute.KindLocal, Address: "http://192.168.1.10:11434"}
	tailnet := compute.Node{Name: "gpu-tail", Kind: compute.KindLocal, Address: "https://gpu-box.tailnet.ts.net:11434"}

	vLan, err := r.Resolve(lan)
	if err != nil {
		t.Fatalf("Resolve(lan): %v", err)
	}
	vTail, err := r.Resolve(tailnet)
	if err != nil {
		t.Fatalf("Resolve(tailnet): %v", err)
	}
	if vLan != vTail {
		t.Fatalf("same node via different addresses: %q != %q", vLan, vTail)
	}

	key, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}
	h1, err := Hash(key, KindNode, vLan)
	if err != nil {
		t.Fatalf("Hash(lan): %v", err)
	}
	h2, err := Hash(key, KindNode, vTail)
	if err != nil {
		t.Fatalf("Hash(tailnet): %v", err)
	}
	if h1 != h2 {
		t.Fatalf("same value under one PairKey must hash identically: %q != %q", h1, h2)
	}
}

func TestResolveDifferentNodesDiffer(t *testing.T) {
	infoA := map[string]any{"gpu_uuids": []string{"GPU-aaaaaaaa-0000-0000-0000-00000000000a"}}
	infoB := map[string]any{"gpu_uuids": []string{"GPU-bbbbbbbb-0000-0000-0000-00000000000b"}}
	nodeA := compute.Node{Name: "node-a", Kind: compute.KindLocal}
	nodeB := compute.Node{Name: "node-b", Kind: compute.KindLocal}

	ra, rb := localResolver(infoA), localResolver(infoB)
	va, err := ra.Resolve(nodeA)
	if err != nil {
		t.Fatalf("Resolve(node-a): %v", err)
	}
	vb, err := rb.Resolve(nodeB)
	if err != nil {
		t.Fatalf("Resolve(node-b): %v", err)
	}
	if va == vb {
		t.Fatalf("different nodes gave identical values: %q", va)
	}

	// Different raw values must also hash differently under one pair
	// key — a same-hash would be a false positive match.
	key, err := PairKey("token-a")
	if err != nil {
		t.Fatalf("PairKey: %v", err)
	}
	ha, err := Hash(key, KindNode, va)
	if err != nil {
		t.Fatalf("Hash(a): %v", err)
	}
	hb, err := Hash(key, KindNode, vb)
	if err != nil {
		t.Fatalf("Hash(b): %v", err)
	}
	if ha == hb {
		t.Fatalf("different values under one PairKey must hash differently: %q", ha)
	}
}

func TestResolvePrefersGPUUUID(t *testing.T) {
	// Both identity sources present: the GPU UUIDs win.
	r := localResolver(sameNodeInfo())
	v, err := r.Resolve(compute.Node{Name: "n", Kind: compute.KindLocal})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := "gpu:GPU-0f88de7f-9c8b-0d4a-c6e0-ba9e5c1d0a2f,GPU-1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"
	if v != want {
		t.Fatalf("value = %q want %q (GPU UUIDs must precede the instance id)", v, want)
	}
}

func TestResolveFallsBackToInstanceID(t *testing.T) {
	info := map[string]any{
		"gpu_uuids":        []string{}, // present but empty — no GPU
		"node_instance_id": "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6",
	}
	r := localResolver(info)
	v, err := r.Resolve(compute.Node{Name: "n", Kind: compute.KindLocal})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := "inst:a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"; v != want {
		t.Fatalf("value = %q want %q", v, want)
	}
}

func TestResolveNoSourceSkipped(t *testing.T) {
	// Neither an observer peer nor a local kind: Resolve says no,
	// ResolveAll skips the node without failing.
	resolver := &IdentityResolver{
		Nodes: func() []compute.Node {
			return []compute.Node{
				{Name: "bound", Kind: compute.KindOllama, ObserverPeer: "obs-1"},
				{Name: "orphan", Kind: compute.KindOllama},
			}
		},
		HostInfo: func(observerPeer string) map[string]any {
			return map[string]any{"node_instance_id": "f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0"}
		},
	}
	if v, err := resolver.Resolve(compute.Node{Name: "orphan", Kind: compute.KindOllama}); err != ErrNoIdentity {
		t.Fatalf("Resolve(orphan) err = %v want ErrNoIdentity", err)
	} else if v != "" {
		t.Fatalf("Resolve(orphan) value = %q want empty", v)
	}
	values := resolver.ResolveAll()
	if len(values) != 1 {
		t.Fatalf("ResolveAll returned %d nodes want 1 (unresolvable node skipped): %+v", len(values), values)
	}
	if values[0].Name != "bound" || values[0].Value != "inst:f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0" {
		t.Fatalf("ResolveAll = %+v want the bound node with its instance id", values)
	}
	// A host that reports neither source is also skipped.
	empty := &IdentityResolver{
		Nodes: func() []compute.Node { return []compute.Node{{Name: "bare", Kind: compute.KindLocal}} },
		Local: func() map[string]any { return map[string]any{"gpu_uuids": []string{}, "node_instance_id": ""} },
	}
	if v, err := empty.Resolve(empty.Nodes()[0]); err != ErrNoIdentity {
		t.Fatalf("host with neither source: got value %q err %v want ErrNoIdentity", v, err)
	}
	if got := empty.ResolveAll(); len(got) != 0 {
		t.Errorf("ResolveAll = %+v want empty", got)
	}
}

// TestResolveIgnoresNameAndAddress pins the contract: the resolved
// value is built solely from host-reported identity — regenerating it
// under different registry names/addresses yields the same value.
func TestResolveIgnoresNameAndAddress(t *testing.T) {
	r := localResolver(sameNodeInfo())
	entries := []compute.Node{
		{Name: "a", Kind: compute.KindLocal, Address: "http://10.0.0.1:11434"},
		{Name: "b-c.d9", Kind: compute.KindLocal, Address: "https://100.64.0.9:11434"},
	}
	first, err := r.Resolve(entries[0])
	if err != nil {
		t.Fatalf("Resolve(first): %v", err)
	}
	for _, n := range entries[1:] {
		got, err := r.Resolve(n)
		if err != nil {
			t.Fatalf("Resolve(%q): %v", n.Name, err)
		}
		if got != first {
			t.Fatalf("same host info resolved differently for %q: %q != %q", n.Name, got, first)
		}
	}
	want := "gpu:GPU-0f88de7f-9c8b-0d4a-c6e0-ba9e5c1d0a2f,GPU-1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d"
	if first != want {
		t.Fatalf("value = %q want %q (must be host-derived, not name/address-derived)", first, want)
	}
}
