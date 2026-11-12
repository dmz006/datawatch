package compute

import "testing"

const testGiB = uint64(1) << 30

// fakeGPUs builds one GPU per entry, each with the given total and used memory.
func fakeGPUs(totalGiB uint64, usedGiB ...uint64) []GPUMemory {
	gpus := make([]GPUMemory, 0, len(usedGiB))
	for _, u := range usedGiB {
		gpus = append(gpus, GPUMemory{TotalBytes: totalGiB * testGiB, UsedBytes: u * testGiB})
	}
	return gpus
}

func TestNodeSlotsOverrideWins(t *testing.T) {
	obs := NodeObservation{GPUs: fakeGPUs(24, 0, 0), LoadedModelBytes: []uint64{20 * testGiB}}

	if got := NodeSlots(3, 0, 1, 0, obs); got != 3 {
		t.Errorf("override 3 with two observed GPUs: got %d, want 3", got)
	}
	if got := NodeSlots(3, 2, 1, 0, obs); got != 2 {
		t.Errorf("override 3 under ceiling 2: got %d, want 2", got)
	}
	if got := NodeSlots(3, 0, 1, 5, obs); got != 3 {
		t.Errorf("override 3 with 5 held: got %d, want 3 (the operator value is not floored)", got)
	}
	if got := NodeSlots(2, 0, 1, 0, NodeObservation{}); got != 2 {
		t.Errorf("override 2 with no observer data: got %d, want 2", got)
	}
}

func TestNodeSlotsFromObserverGPUsAndMemory(t *testing.T) {
	cases := []struct {
		name    string
		ceiling int
		held    int
		obs     NodeObservation
		want    int
	}{
		{
			name: "one slot per GPU when no model size is known",
			obs:  NodeObservation{GPUs: fakeGPUs(24, 0, 0)},
			want: 2,
		},
		{
			name: "no model size known, so a full GPU is not counted out",
			obs:  NodeObservation{GPUs: fakeGPUs(24, 22, 22)},
			want: 2,
		},
		{
			name: "the node's own loaded model is subtracted like any other memory in use",
			obs: NodeObservation{
				GPUs:             fakeGPUs(24, 20, 0),
				LoadedModelBytes: []uint64{20 * testGiB},
			},
			want: 1,
		},
		{
			name: "a foreign program holding memory on every GPU lowers the count",
			obs: NodeObservation{
				GPUs:           fakeGPUs(24, 10, 10),
				LastModelBytes: 20 * testGiB,
			},
			want: 1,
		},
		{
			name: "the last served model size applies while no model is loaded",
			obs: NodeObservation{
				GPUs:           fakeGPUs(24, 0, 0),
				LastModelBytes: 20 * testGiB,
			},
			want: 2,
		},
		{
			name: "both GPUs nearly full cuts the count to the memory that is free",
			obs: NodeObservation{
				GPUs:             fakeGPUs(24, 22, 22),
				LoadedModelBytes: []uint64{20 * testGiB},
			},
			want: 1,
		},
		{
			name: "running workers floor the derived count",
			held: 2,
			obs: NodeObservation{
				GPUs:             fakeGPUs(24, 22, 22),
				LoadedModelBytes: []uint64{20 * testGiB},
			},
			want: 2,
		},
		{
			name:    "max_concurrent_sessions caps the derived count",
			ceiling: 1,
			obs:     NodeObservation{GPUs: fakeGPUs(24, 0, 0)},
			want:    1,
		},
		{
			name: "GPUs without memory figures get one slot each",
			obs: NodeObservation{
				GPUs:             []GPUMemory{{}, {}},
				LoadedModelBytes: []uint64{20 * testGiB},
			},
			want: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NodeSlots(0, tc.ceiling, 1, tc.held, tc.obs); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNodeSlotsDefaultOne(t *testing.T) {
	cases := []struct {
		name     string
		fallback int
		ceiling  int
		want     int
	}{
		{name: "no observer data and no operator value takes one slot", fallback: 1, want: 1},
		{name: "an unset fallback still takes one slot", fallback: 0, want: 1},
		{name: "no observer data takes autonomous.max_engines_per_node", fallback: 2, want: 2},
		{name: "the fallback is capped by max_concurrent_sessions", fallback: 2, ceiling: 1, want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NodeSlots(0, tc.ceiling, tc.fallback, 0, NodeObservation{}); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}

	// An observer report with no GPU (a CPU-only node) is not GPU data, so the
	// fallback applies.
	cpuOnly := NodeObservation{LoadedModelBytes: []uint64{8 * testGiB}}
	if got := NodeSlots(0, 0, 1, 0, cpuOnly); got != 1 {
		t.Errorf("CPU-only observer data: got %d, want 1", got)
	}
}

func TestSingleGPUNodeStaysAtOneSlot(t *testing.T) {
	cases := []struct {
		name string
		held int
		obs  NodeObservation
	}{
		{
			name: "empty large GPU",
			obs:  NodeObservation{GPUs: fakeGPUs(80, 0)},
		},
		{
			name: "large GPU with a model loaded",
			obs: NodeObservation{
				GPUs:             fakeGPUs(80, 10),
				LoadedModelBytes: []uint64{10 * testGiB},
			},
		},
		{
			name: "GPU full with the node's own model loaded",
			obs: NodeObservation{
				GPUs:             fakeGPUs(24, 24),
				LoadedModelBytes: []uint64{20 * testGiB},
			},
		},
		{
			name: "GPU full with only the last served model size known",
			obs: NodeObservation{
				GPUs:           fakeGPUs(24, 24),
				LastModelBytes: 20 * testGiB,
			},
		},
		{
			name: "a worker already running",
			held: 1,
			obs:  NodeObservation{GPUs: fakeGPUs(80, 10), LoadedModelBytes: []uint64{10 * testGiB}},
		},
		{
			name: "two workers already running",
			held: 2,
			obs:  NodeObservation{GPUs: fakeGPUs(80, 10), LoadedModelBytes: []uint64{10 * testGiB}},
		},
		{
			name: "three workers already running on a full GPU",
			held: 3,
			obs: NodeObservation{
				GPUs:             fakeGPUs(24, 24),
				LoadedModelBytes: []uint64{20 * testGiB},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The fallback is set high to show the observer GPU data governs.
			if got := NodeSlots(0, 0, 4, tc.held, tc.obs); got != 1 {
				t.Errorf("single GPU: got %d, want 1", got)
			}
		})
	}
}

// TestNodeSlotsCPUOnlyFromSystemMemory covers a hosting node with no GPU data:
// free system memory divided by the loaded model's size, at least one.
func TestNodeSlotsCPUOnlyFromSystemMemory(t *testing.T) {
	obs := NodeObservation{
		SystemTotalBytes: 64 * testGiB,
		SystemUsedBytes:  16 * testGiB,
		CPUModelBytes:    []uint64{12 * testGiB},
	}
	if got := NodeSlots(0, 0, 1, 0, obs); got != 4 {
		t.Errorf("48 GiB free, 12 GiB model: got %d, want 4", got)
	}
	if got := NodeSlots(0, 2, 1, 0, obs); got != 2 {
		t.Errorf("4 derived under ceiling 2: got %d, want 2", got)
	}
	if got := NodeSlots(6, 0, 1, 0, obs); got != 6 {
		t.Errorf("override 6 on a CPU-only node: got %d, want 6", got)
	}

	full := obs
	full.SystemUsedBytes = 60 * testGiB
	if got := NodeSlots(0, 0, 3, 0, full); got != 1 {
		t.Errorf("4 GiB free, 12 GiB model: got %d, want floor 1", got)
	}

	remembered := NodeObservation{
		SystemTotalBytes:  64 * testGiB,
		SystemUsedBytes:   16 * testGiB,
		LastCPUModelBytes: 16 * testGiB,
	}
	if got := NodeSlots(0, 0, 1, 0, remembered); got != 3 {
		t.Errorf("48 GiB free, remembered 16 GiB model: got %d, want 3", got)
	}

	noModel := NodeObservation{SystemTotalBytes: 64 * testGiB, SystemUsedBytes: 16 * testGiB}
	if got := NodeSlots(0, 0, 5, 0, noModel); got != 5 {
		t.Errorf("no model size: got %d, want fallback 5", got)
	}
	noMem := NodeObservation{CPUModelBytes: []uint64{12 * testGiB}}
	if got := NodeSlots(0, 0, 5, 0, noMem); got != 5 {
		t.Errorf("no system memory: got %d, want fallback 5", got)
	}
}
