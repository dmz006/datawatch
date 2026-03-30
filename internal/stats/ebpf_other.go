//go:build !linux

package stats

import "fmt"

// EBPFCollector is a no-op stub on non-Linux platforms.
type EBPFCollector struct{}

// NewEBPFCollector returns an error on non-Linux platforms.
func NewEBPFCollector() (*EBPFCollector, error) {
	return nil, fmt.Errorf("eBPF is only supported on Linux")
}

// Close is a no-op on non-Linux platforms.
func (c *EBPFCollector) Close() error { return nil }

// ReadPIDTreeBytes returns zero on non-Linux platforms.
func (c *EBPFCollector) ReadPIDTreeBytes(pid uint32) (tx, rx uint64) { return 0, 0 }

// DumpStats is a no-op on non-Linux platforms.
func (c *EBPFCollector) DumpStats() (txEntries, rxEntries int) { return 0, 0 }

func HasCapBPF(binaryPath string) bool { return false }

func SetCapBPF(binaryPath string) error {
	return fmt.Errorf("eBPF is only supported on Linux")
}

func CheckEBPFReady(binaryPath string) error {
	return fmt.Errorf("eBPF is only supported on Linux")
}
