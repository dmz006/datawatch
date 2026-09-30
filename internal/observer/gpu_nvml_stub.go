// gpu_nvml_stub.go — NVMLProbe no-op for non-Linux platforms.
// NVML only exists on Linux; on other platforms NewNVMLProbe returns nil and
// callers fall through to TegraStatsProbe or SMIProbe.

//go:build !linux

package observer

import (
	"context"
	"time"
)

// NVMLProbe is a no-op on non-Linux platforms.
type NVMLProbe struct{}

// NewNVMLProbe always returns nil on non-Linux.
func NewNVMLProbe(_ time.Duration) *NVMLProbe { return nil }

// NVMLInitError mirrors the linux build's diagnostic hook; NVML is
// Linux-only, so this platform never has a failure reason to report.
func NVMLInitError() string { return "" }

func (p *NVMLProbe) Start(_ context.Context) {}
func (p *NVMLProbe) Stop()                   {}
func (p *NVMLProbe) Latest() []GPU           { return nil }
func (p *NVMLProbe) LastError() string       { return "" }
