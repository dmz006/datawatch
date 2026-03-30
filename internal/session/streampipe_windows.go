//go:build windows

package session

import "errors"

type StreamingPipe struct {
	OnRawData func(data []byte)
	OnLine    func(line string)
}

func NewStreamingPipe(logPath string) (*StreamingPipe, error) {
	return nil, errors.New("streaming pipe not available on Windows")
}

func (sp *StreamingPipe) FIFOPath() string { return "" }
func (sp *StreamingPipe) Close()           {}
