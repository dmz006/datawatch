// gpu_tegrastats.go — tegrastats probe for NVIDIA Jetson/Tegra SoCs.
//
// Reads one output line from `tegrastats --interval 500` (killed by
// context timeout after first line). Handles two output formats:
//
//   Classic Jetson (Orin/Nano/TX2): GR3D_FREQ X%@Y GPU@T.TC RAM X/YMB
//   Tegra Thor (GB10/GH200):        gpu@T.TC RAM X/YMB VDD_GPU XmW/YmW
//                                   (no explicit GPU util%)
//
// Degrades cleanly when tegrastats is not in PATH (NewTegraStatsProbe
// returns nil).

package observer

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TegraStatsProbe polls tegrastats for Jetson/Tegra GPU stats.
// Nil-safe on all methods.
type TegraStatsProbe struct {
	interval time.Duration
	mu       sync.RWMutex
	latest   []GPU
	stopCh   chan struct{}
}

// NewTegraStatsProbe returns a probe if tegrastats is in PATH, nil otherwise.
func NewTegraStatsProbe(interval time.Duration) *TegraStatsProbe {
	if _, err := exec.LookPath("tegrastats"); err != nil {
		return nil
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &TegraStatsProbe{interval: interval, stopCh: make(chan struct{})}
}

// Start begins polling in a background goroutine.
func (p *TegraStatsProbe) Start(ctx context.Context) {
	if p == nil {
		return
	}
	go func() {
		p.poll(ctx)
		t := time.NewTicker(p.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.stopCh:
				return
			case <-t.C:
				p.poll(ctx)
			}
		}
	}()
}

// Stop cancels the background goroutine. Idempotent.
func (p *TegraStatsProbe) Stop() {
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
func (p *TegraStatsProbe) Latest() []GPU {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]GPU, len(p.latest))
	copy(out, p.latest)
	return out
}

func (p *TegraStatsProbe) poll(ctx context.Context) {
	// tegrastats runs continuously; context timeout kills it after we
	// have at least one line of output. --interval sets ms between lines.
	ctx2, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx2, "tegrastats", "--interval", "500")
	out, _ := cmd.Output() // non-zero exit expected when killed by timeout
	if len(out) == 0 {
		return
	}
	// Take the first complete line (skip timestamp prefix lines if any).
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		gpus := parseTegraStatsLine(line)
		if len(gpus) > 0 {
			p.mu.Lock()
			p.latest = gpus
			p.mu.Unlock()
			return
		}
	}
}

// Regexes cover both classic Jetson (GR3D_FREQ / GPU@) and Tegra Thor
// (no GR3D_FREQ / gpu@ lowercase / VDD_GPU mW).
var (
	// Classic Jetson: "GR3D_FREQ 42%@114"
	reTegraGR3D = regexp.MustCompile(`GR3D_FREQ\s+(\d+)%`)

	// RAM field common to all formats: "RAM 69178/125772MB"
	reTegraRAM = regexp.MustCompile(`RAM\s+(\d+)/(\d+)MB`)

	// Temperature: "GPU@49.25C" (classic) or "gpu@35.25C" (Thor)
	reTegraGPUT = regexp.MustCompile(`(?i)gpu@([\d.]+)C`)

	// Power (Thor/GH200): "VDD_GPU 2376mW/2376mW" — first value is current draw
	reTegraPower = regexp.MustCompile(`VDD_GPU\s+(\d+)mW`)
)

// parseTegraStatsLine extracts GPU stats from one tegrastats output line.
// Returns nil when neither a GPU temperature nor GR3D_FREQ field is found
// (i.e. the line is not a valid tegrastats stats line).
//
// Classic Jetson example:
//
//	RAM 2928/7624MB ... GR3D_FREQ 42%@114 ... GPU@49.25C ...
//
// Tegra Thor example:
//
//	09-12-2026 18:08:03 RAM 69178/125772MB ... gpu@35.25C VDD_GPU 2376mW/2376mW
func parseTegraStatsLine(line string) []GPU {
	// Must find at least a GPU temperature to consider this a valid line.
	tM := reTegraGPUT.FindStringSubmatch(line)
	if tM == nil {
		return nil
	}
	tempC, _ := strconv.ParseFloat(tM[1], 64)

	var utilPct float64
	if utilM := reTegraGR3D.FindStringSubmatch(line); utilM != nil {
		utilPct, _ = strconv.ParseFloat(utilM[1], 64)
	}

	var memUsed, memTotal uint64
	if ramM := reTegraRAM.FindStringSubmatch(line); ramM != nil {
		u, _ := strconv.ParseUint(ramM[1], 10, 64)
		t, _ := strconv.ParseUint(ramM[2], 10, 64)
		memUsed = u * 1024 * 1024
		memTotal = t * 1024 * 1024
	}

	var powerW float64
	if pM := reTegraPower.FindStringSubmatch(line); pM != nil {
		mw, _ := strconv.ParseFloat(pM[1], 64)
		powerW = mw / 1000.0
	}

	return []GPU{{
		Name:          "Tegra GPU",
		Vendor:        "nvidia",
		UtilPct:       utilPct,
		MemUsedBytes:  memUsed,
		MemTotalBytes: memTotal,
		TempC:         tempC,
		PowerW:        powerW,
	}}
}
