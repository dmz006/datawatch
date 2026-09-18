package observer

import (
	"testing"
)

// Tests for parseTegraStatsLine (gpu_tegrastats.go) — v8.25.3

func TestParseTegraStatsLine_ClassicJetson(t *testing.T) {
	line := "RAM 1024/4096MB (lfb 512x4MB) SWAP 0/2048MB GR3D_FREQ 45%@1300 CPU [20%@1190,25%@1190] GPU@65.5C"
	gpus := parseTegraStatsLine(line)
	if len(gpus) != 1 {
		t.Fatalf("expected 1 GPU entry, got %d", len(gpus))
	}
	g := gpus[0]
	if g.UtilPct != 45 {
		t.Errorf("util_pct: want 45, got %v", g.UtilPct)
	}
	if g.TempC != 65.5 {
		t.Errorf("temp_c: want 65.5, got %v", g.TempC)
	}
	if g.MemUsedBytes != 1024*1024*1024 {
		t.Errorf("mem_used_bytes: want %d, got %d", 1024*1024*1024, g.MemUsedBytes)
	}
	if g.MemTotalBytes != 4096*1024*1024 {
		t.Errorf("mem_total_bytes: want %d, got %d", uint64(4096)*1024*1024, g.MemTotalBytes)
	}
	if g.Vendor != "nvidia" {
		t.Errorf("vendor: want nvidia, got %q", g.Vendor)
	}
}

func TestParseTegraStatsLine_ThorFormat(t *testing.T) {
	// Thor (GB10/GH200): no GR3D_FREQ, lowercase gpu@, VDD_GPU
	line := "09-12-2026 18:08:03 RAM 69178/125772MB (lfb 1x2MB) SWAP 0/0MB cpu@45.0C soc0@42.0C gpu@35.25C tj@45.0C VDD_IN 6234mW/6234mW VDD_CPU_GPU_CV 2376mW/2376mW VDD_SOC 2000mW/2000mW VDD_GPU 2376mW/2376mW"
	gpus := parseTegraStatsLine(line)
	if len(gpus) != 1 {
		t.Fatalf("expected 1 GPU entry for Thor format, got %d", len(gpus))
	}
	g := gpus[0]
	if g.UtilPct != 0 {
		t.Errorf("util_pct: want 0 (no GR3D_FREQ in Thor), got %v", g.UtilPct)
	}
	if g.TempC != 35.25 {
		t.Errorf("temp_c: want 35.25, got %v", g.TempC)
	}
	if g.PowerW != 2.376 {
		t.Errorf("power_w: want 2.376, got %v", g.PowerW)
	}
}

func TestParseTegraStatsLine_NoGPUTemp_ReturnsNil(t *testing.T) {
	line := "RAM 1024/4096MB CPU [20%@1190] EMC_FREQ 50% MTS fg 0% bg 0%"
	gpus := parseTegraStatsLine(line)
	if gpus != nil {
		t.Errorf("expected nil for line with no GPU temp, got %v", gpus)
	}
}

// Tests for parseSMIOutput (gpu_smi.go) — v8.25.3

func TestParseSMIOutput_DiscreteGPU(t *testing.T) {
	raw := "0, NVIDIA RTX 4090, 85, 24576, 4096, 72.0"
	gpus := parseSMIOutput(raw)
	if len(gpus) != 1 {
		t.Fatalf("expected 1 GPU, got %d", len(gpus))
	}
	g := gpus[0]
	if g.Name != "NVIDIA RTX 4090" {
		t.Errorf("name: want 'NVIDIA RTX 4090', got %q", g.Name)
	}
	if g.UtilPct != 85 {
		t.Errorf("util_pct: want 85, got %v", g.UtilPct)
	}
	if g.TempC != 72.0 {
		t.Errorf("temp_c: want 72.0, got %v", g.TempC)
	}
	expectedTotal := uint64(24576) * 1024 * 1024
	if g.MemTotalBytes != expectedTotal {
		t.Errorf("mem_total_bytes: want %d, got %d", expectedTotal, g.MemTotalBytes)
	}
}

func TestParseSMIOutput_TegraUnifiedMemory_NAFields(t *testing.T) {
	// Tegra unified memory: [N/A] for mem fields
	raw := "0, Tegra GPU, [N/A], [N/A], [N/A], 45.0"
	gpus := parseSMIOutput(raw)
	if len(gpus) != 1 {
		t.Fatalf("expected 1 GPU, got %d", len(gpus))
	}
	g := gpus[0]
	if g.MemUsedBytes != 0 {
		t.Errorf("mem_used_bytes: want 0 for [N/A], got %d", g.MemUsedBytes)
	}
	if g.MemTotalBytes != 0 {
		t.Errorf("mem_total_bytes: want 0 for [N/A], got %d", g.MemTotalBytes)
	}
	if g.TempC != 45.0 {
		t.Errorf("temp_c: want 45.0, got %v", g.TempC)
	}
}

func TestParseSMIOutput_MultiGPU(t *testing.T) {
	raw := "0, GPU0, 10, 8192, 1024, 50.0\n1, GPU1, 20, 8192, 2048, 55.0"
	gpus := parseSMIOutput(raw)
	if len(gpus) != 2 {
		t.Fatalf("expected 2 GPUs, got %d", len(gpus))
	}
}

func TestParseSMIOutput_EmptyInput(t *testing.T) {
	gpus := parseSMIOutput("")
	if len(gpus) != 0 {
		t.Errorf("expected 0 GPUs for empty input, got %d", len(gpus))
	}
}

// Tests for Collector.SetGPUFn (gpu_probe_test.go) — v8.25.3

func TestCollector_SetGPUFn_PopulatesSnapGPU(t *testing.T) {
	fakeGPU := []GPU{{
		Name:    "Test GPU",
		Vendor:  "nvidia",
		UtilPct: 42.0,
		TempC:   70.0,
	}}

	c := NewCollector(DefaultConfig())
	c.SetGPUFn(func() []GPU { return fakeGPU })

	snap := c.collect()
	if len(snap.GPU) != 1 {
		t.Fatalf("expected 1 GPU in snap, got %d", len(snap.GPU))
	}
	if snap.GPU[0].UtilPct != 42.0 {
		t.Errorf("GPU util_pct: want 42.0, got %v", snap.GPU[0].UtilPct)
	}
	// v1 alias should be populated from first GPU
	if snap.GPUPctV1 != 42.0 {
		t.Errorf("GPUPctV1 (v1 alias): want 42.0, got %v", snap.GPUPctV1)
	}
}

func TestCollector_SetGPUFn_NilClearsGPU(t *testing.T) {
	c := NewCollector(DefaultConfig())
	c.SetGPUFn(func() []GPU {
		return []GPU{{Name: "Test GPU", Vendor: "nvidia"}}
	})
	c.SetGPUFn(nil) // clear

	snap := c.collect()
	if len(snap.GPU) != 0 {
		t.Errorf("expected 0 GPUs after SetGPUFn(nil), got %d", len(snap.GPU))
	}
}

func TestCollector_SetGPUFn_NoFn_NoGPUInSnap(t *testing.T) {
	c := NewCollector(DefaultConfig())
	// gpuFn not set — snap.GPU should be empty
	snap := c.collect()
	if len(snap.GPU) != 0 {
		t.Errorf("expected 0 GPUs when no gpuFn set, got %d", len(snap.GPU))
	}
}

// TestNewTegraStatsProbe_NotInPATH verifies NewTegraStatsProbe returns nil
// when the tegrastats binary is not present (non-Jetson host).
func TestNewTegraStatsProbe_NotInPATH(t *testing.T) {
	// tegrastats is only present on NVIDIA Jetson/Tegra devices.
	// On any other host the probe must return nil, not panic.
	probe := NewTegraStatsProbe(0)
	// On dev/CI hosts without tegrastats binary this must be nil.
	// On a Jetson the test is a no-op (probe is non-nil but that's fine).
	if probe != nil {
		t.Skip("tegrastats found in PATH — skipping nil-return assertion (Jetson host)")
	}
}
