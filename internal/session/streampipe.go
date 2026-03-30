//go:build !windows

// Package session provides a StreamingPipe that reads tmux output from a FIFO
// in real-time and dispatches raw bytes to callbacks.
package session

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// StreamingPipe reads from a FIFO in real-time and dispatches raw bytes.
// It also writes to a log file for persistence.
type StreamingPipe struct {
	fifoPath string
	logFile  *os.File
	stopCh   chan struct{}
	doneCh   chan struct{}

	// OnRawData is called with each chunk of raw bytes (for WebSocket/xterm.js).
	OnRawData func(data []byte)

	// OnLine is called with each newline-terminated line (for state detection).
	// The line includes the trailing newline.
	OnLine func(line string)
}

// NewStreamingPipe creates a FIFO and starts reading from it.
// logPath is where bytes are persisted. The FIFO is at logPath + ".pipe".
func NewStreamingPipe(logPath string) (*StreamingPipe, error) {
	fifoPath := logPath + ".pipe"

	// Remove stale FIFO
	os.Remove(fifoPath)

	if err := syscall.Mkfifo(fifoPath, 0600); err != nil {
		return nil, fmt.Errorf("mkfifo %s: %w", fifoPath, err)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Remove(fifoPath)
		return nil, fmt.Errorf("open log %s: %w", logPath, err)
	}

	sp := &StreamingPipe{
		fifoPath: fifoPath,
		logFile:  logFile,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	go sp.readLoop()
	return sp, nil
}

// FIFOPath returns the path to the FIFO for tmux pipe-pane.
func (sp *StreamingPipe) FIFOPath() string {
	return sp.fifoPath
}

// Close stops the read loop and cleans up.
func (sp *StreamingPipe) Close() {
	close(sp.stopCh)
	// Unblock the FIFO open by writing to it
	f, err := os.OpenFile(sp.fifoPath, os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err == nil {
		f.Close()
	}
	<-sp.doneCh
	sp.logFile.Close()
	os.Remove(sp.fifoPath)
}

func (sp *StreamingPipe) readLoop() {
	defer close(sp.doneCh)

	buf := make([]byte, 64*1024)
	var lineBuf []byte // accumulates partial lines for OnLine callback

	for {
		select {
		case <-sp.stopCh:
			return
		default:
		}

		// Open FIFO for reading — blocks until tmux pipe-pane connects
		file, err := os.OpenFile(sp.fifoPath, os.O_RDONLY, 0)
		if err != nil {
			select {
			case <-sp.stopCh:
				return
			default:
			}
			continue
		}

		for {
			select {
			case <-sp.stopCh:
				file.Close()
				return
			default:
			}

			n, err := file.Read(buf)
			if n > 0 {
				chunk := buf[:n]

				// Write to log file for persistence
				sp.logFile.Write(chunk) //nolint:errcheck

				// Dispatch raw bytes for WebSocket/xterm.js
				if sp.OnRawData != nil {
					sp.OnRawData(chunk)
				}

				// Accumulate lines for state detection
				if sp.OnLine != nil {
					lineBuf = append(lineBuf, chunk...)
					for {
						nlIdx := -1
						for i, b := range lineBuf {
							if b == '\n' {
								nlIdx = i
								break
							}
						}
						if nlIdx < 0 {
							break
						}
						line := string(lineBuf[:nlIdx+1])
						lineBuf = lineBuf[nlIdx+1:]
						sp.OnLine(line)
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					break
				}
				// EOF means pipe-pane disconnected (tmux session closed)
				break
			}
		}
		file.Close()
	}
}
