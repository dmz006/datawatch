// gpu_smi.go — nvidia-smi based GPU probe for Shape A/B hosts.
//
// Polls nvidia-smi every 5 s and caches the result. Degrades cleanly
// when nvidia-smi is not in PATH — NewSMIProbe returns nil and all
// nil-receiver methods are no-ops. On Tegra/Thor SoCs, memory fields
// show 0 (unified memory has no discrete VRAM; use TegraStatsProbe
// for system-RAM stats).

package observer

import (
	"context"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SMIProbe polls nvidia-smi and caches GPU stats. Nil-safe.
type SMIProbe struct {
	interval time.Duration
	mu       sync.RWMutex
	latest   []GPU
	lastErr  string
	stopCh   chan struct{}
}

// NewSMIProbe returns a probe if nvidia-smi is in PATH, nil otherwise.
func NewSMIProbe(interval time.Duration) *SMIProbe {
	if _, err := exec.LookPath("nvidia-smi"); err != nil {
		return nil
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	return &SMIProbe{interval: interval, stopCh: make(chan struct{})}
}

// Start begins polling in a background goroutine.
func (p *SMIProbe) Start(ctx context.Context) {
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
func (p *SMIProbe) Stop() {
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
func (p *SMIProbe) Latest() []GPU {
	if p == nil {
		return nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]GPU, len(p.latest))
	copy(out, p.latest)
	return out
}

func (p *SMIProbe) poll(ctx context.Context) {
	ctx2, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx2, "nvidia-smi",
		"--query-gpu=index,name,utilization.gpu,memory.total,memory.used,temperature.gpu",
		"--format=csv,noheader,nounits")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// nvidia-smi prints the real reason (e.g. "Failed to initialize NVML:
		// Driver/library version mismatch") to stderr, not in the exec error
		// itself (just "exit status N") — surface that text so a broken
		// driver is distinguishable from "no GPU present" everywhere this
		// probe's data reaches: /api/observer/stats, /api/compute/nodes, and
		// the MCP tools built on top of them.
		p.mu.Lock()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			p.lastErr = msg
		} else {
			p.lastErr = err.Error()
		}
		p.mu.Unlock()
		return
	}
	gpus := parseSMIOutput(string(out))
	p.mu.Lock()
	p.latest = gpus
	p.lastErr = ""
	p.mu.Unlock()
}

// LastError returns the most recent nvidia-smi failure reason, or "" if the
// last poll succeeded (or none has run yet). Nil-safe.
func (p *SMIProbe) LastError() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.lastErr
}

// parseSMIOutput converts CSV output from nvidia-smi into GPU entries.
// Fields: index, name, util%, mem_total MiB, mem_used MiB, temp °C.
// [N/A] values (Tegra unified memory) parse to 0.
func parseSMIOutput(raw string) []GPU {
	mibToBytes := func(s string) uint64 {
		s = strings.TrimSpace(s)
		if s == "" || s == "[N/A]" || s == "N/A" || s == "[Not Supported]" {
			return 0
		}
		v, _ := strconv.ParseFloat(s, 64)
		return uint64(math.Round(v * 1024 * 1024))
	}
	parseF := func(s string) float64 {
		s = strings.TrimSpace(s)
		if s == "" || s == "[N/A]" || s == "N/A" || s == "[Not Supported]" {
			return 0
		}
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}

	var gpus []GPU
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		f := strings.Split(line, ", ")
		if len(f) < 6 {
			continue
		}
		gpus = append(gpus, GPU{
			Name:          strings.TrimSpace(f[1]),
			Vendor:        "nvidia",
			UtilPct:       parseF(f[2]),
			MemTotalBytes: mibToBytes(f[3]),
			MemUsedBytes:  mibToBytes(f[4]),
			TempC:         parseF(f[5]),
		})
	}
	return gpus
}
