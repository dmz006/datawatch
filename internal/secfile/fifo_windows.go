//go:build windows

package secfile

import "fmt"

// EncryptingFIFO is not supported on Windows (no named pipes via mkfifo).
type EncryptingFIFO struct{}

func NewEncryptingFIFO(fifoPath, outputPath string, key []byte) (*EncryptingFIFO, error) {
	return nil, fmt.Errorf("encrypted FIFO logging not supported on Windows")
}

func (f *EncryptingFIFO) FIFOPath() string { return "" }
func (f *EncryptingFIFO) Close() error     { return nil }
