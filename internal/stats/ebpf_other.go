//go:build !linux

package stats

import "fmt"

func HasCapBPF(binaryPath string) bool { return false }

func SetCapBPF(binaryPath string) error {
	return fmt.Errorf("eBPF is only supported on Linux")
}

func CheckEBPFReady(binaryPath string) error {
	return fmt.Errorf("eBPF is only supported on Linux")
}
