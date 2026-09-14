// gpu_nvml_linux.go — NVML direct-binding GPU probe (Linux only, no CGO).
//
// Loads libnvidia-ml.so at runtime via purego/dlopen. Returns nil from
// NewNVMLProbe when the library is absent so the caller can fall back to
// tegrastats or nvidia-smi. All methods are nil-safe.
//
// Probe priority in callers: NVML > TegraStats > SMI.
//
// NVML fixes the Thor util=0 bug: tegrastats on Thor has no GR3D_FREQ field
// so utilisation is always 0, whereas NVML reports it natively.

//go:build linux

package observer

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const nvmlSuccess uint32 = 0
const nvmlTemperatureGPU uint32 = 0

// nvmlUtilization matches nvmlUtilization_t { unsigned gpu; unsigned memory; }.
type nvmlUtilization struct {
	GPU    uint32
	Memory uint32
}

// nvmlMemory matches nvmlMemory_t { ulonglong total, free, used; }.
type nvmlMemory struct {
	Total uint64
	Free  uint64
	Used  uint64
}

type nvmlFunctions struct {
	init        func() uint32
	shutdown    func() uint32
	count       func(*uint32) uint32
	handle      func(uint32, *uintptr) uint32
	name        func(uintptr, unsafe.Pointer, uint32) uint32
	utilization func(uintptr, unsafe.Pointer) uint32
	memory      func(uintptr, unsafe.Pointer) uint32
	temperature func(uintptr, uint32, *uint32) uint32
	power       func(uintptr, *uint32) uint32
}

// NVMLProbe queries libnvidia-ml.so directly (no CGO). Nil-safe.
type NVMLProbe struct {
	lib      uintptr
	fns      nvmlFunctions
	interval time.Duration
	mu       sync.RWMutex
	latest   []GPU
	stopCh   chan struct{}
}

// nvmlLibCandidates lists library paths tried in order. The bare name relies
// on the dynamic linker's ldconfig cache; architecture-specific paths are
// fallbacks for non-standard installations.
var nvmlLibCandidates = []string{
	"libnvidia-ml.so.1",
	"libnvidia-ml.so",
	"/usr/lib/x86_64-linux-gnu/libnvidia-ml.so.1",
	"/usr/lib/x86_64-linux-gnu/libnvidia-ml.so",
	"/usr/lib/aarch64-linux-gnu/nvidia/libnvidia-ml.so.1",
	"/usr/lib/aarch64-linux-gnu/libnvidia-ml.so.1",
}

// NewNVMLProbe opens libnvidia-ml.so and registers NVML function bindings.
// Returns nil when the library is not available; callers should fall back to
// TegraStatsProbe or SMIProbe.
func NewNVMLProbe(interval time.Duration) *NVMLProbe {
	var lib uintptr
	for _, path := range nvmlLibCandidates {
		var err error
		lib, err = purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			break
		}
	}
	if lib == 0 {
		return nil
	}

	if interval <= 0 {
		interval = 5 * time.Second
	}
	p := &NVMLProbe{lib: lib, interval: interval, stopCh: make(chan struct{})}

	type sym struct {
		fptr any
		name string
	}
	syms := []sym{
		{&p.fns.init, "nvmlInit_v2"},
		{&p.fns.shutdown, "nvmlShutdown"},
		{&p.fns.count, "nvmlDeviceGetCount_v2"},
		{&p.fns.handle, "nvmlDeviceGetHandleByIndex_v2"},
		{&p.fns.name, "nvmlDeviceGetName"},
		{&p.fns.utilization, "nvmlDeviceGetUtilizationRates"},
		{&p.fns.memory, "nvmlDeviceGetMemoryInfo"},
		{&p.fns.temperature, "nvmlDeviceGetTemperature"},
		{&p.fns.power, "nvmlDeviceGetPowerUsage"},
	}
	for _, s := range syms {
		addr, err := purego.Dlsym(lib, s.name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[nvml] symbol %s not found: %v\n", s.name, err)
			purego.Dlclose(lib) //nolint:errcheck
			return nil
		}
		purego.RegisterFunc(s.fptr, addr)
	}

	if ret := p.fns.init(); ret != nvmlSuccess {
		fmt.Fprintf(os.Stderr, "[nvml] nvmlInit_v2 returned %d\n", ret)
		purego.Dlclose(lib) //nolint:errcheck
		return nil
	}
	return p
}

// Start begins polling in a background goroutine.
func (p *NVMLProbe) Start(ctx context.Context) {
	if p == nil {
		return
	}
	go func() {
		p.poll()
		t := time.NewTicker(p.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stopCh:
				return
			case <-t.C:
				p.poll()
			}
		}
	}()
}

// Stop cancels the background goroutine. Idempotent.
func (p *NVMLProbe) Stop() {
	if p == nil {
		return
	}
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
}

// Latest returns the most recent GPU snapshot. Nil-safe.
func (p *NVMLProbe) Latest() []GPU {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]GPU, len(p.latest))
	copy(out, p.latest)
	return out
}

func (p *NVMLProbe) poll() {
	var count uint32
	if p.fns.count(&count) != nvmlSuccess || count == 0 {
		return
	}

	gpus := make([]GPU, 0, count)
	for i := uint32(0); i < count; i++ {
		var dev uintptr
		if p.fns.handle(i, &dev) != nvmlSuccess {
			continue
		}

		var nameBuf [96]byte
		gpuName := "NVIDIA GPU"
		if p.fns.name(dev, unsafe.Pointer(&nameBuf[0]), 96) == nvmlSuccess {
			gpuName = strings.TrimRight(string(nameBuf[:]), "\x00 ")
		}

		var util nvmlUtilization
		var utilPct float64
		if p.fns.utilization(dev, unsafe.Pointer(&util)) == nvmlSuccess {
			utilPct = float64(util.GPU)
		}

		var mem nvmlMemory
		var memTotal, memUsed uint64
		if p.fns.memory(dev, unsafe.Pointer(&mem)) == nvmlSuccess {
			memTotal = mem.Total
			memUsed = mem.Used
		}

		var tempRaw uint32
		var tempC float64
		if p.fns.temperature(dev, nvmlTemperatureGPU, &tempRaw) == nvmlSuccess {
			tempC = float64(tempRaw)
		}

		var powerMW uint32
		var powerW float64
		if p.fns.power(dev, &powerMW) == nvmlSuccess {
			powerW = float64(powerMW) / 1000.0
		}

		gpus = append(gpus, GPU{
			Name:          gpuName,
			Vendor:        "nvidia",
			UtilPct:       utilPct,
			MemTotalBytes: memTotal,
			MemUsedBytes:  memUsed,
			TempC:         tempC,
			PowerW:        powerW,
		})
	}

	p.mu.Lock()
	p.latest = gpus
	p.mu.Unlock()
}
