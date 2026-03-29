package secfile

import (
	"bufio"
	"fmt"
	"os"
	"syscall"
	"time"
)

// EncryptingFIFO creates a named FIFO that reads plaintext and writes
// encrypted blocks to an output file. Used to encrypt tmux pipe-pane output.
type EncryptingFIFO struct {
	fifoPath   string
	writer     *EncryptedLogWriter
	stopCh     chan struct{}
	doneCh     chan struct{}
}

// NewEncryptingFIFO creates a FIFO at fifoPath and starts a goroutine that
// reads from it and writes encrypted blocks to outputPath.
func NewEncryptingFIFO(fifoPath, outputPath string, key []byte) (*EncryptingFIFO, error) {
	// Remove stale FIFO if exists
	os.Remove(fifoPath)

	// Create named pipe
	if err := syscall.Mkfifo(fifoPath, 0600); err != nil {
		return nil, fmt.Errorf("mkfifo %s: %w", fifoPath, err)
	}

	writer, err := NewEncryptedLogWriter(outputPath, key)
	if err != nil {
		os.Remove(fifoPath)
		return nil, err
	}

	f := &EncryptingFIFO{
		fifoPath: fifoPath,
		writer:   writer,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}

	go f.readLoop()
	return f, nil
}

// FIFOPath returns the path to the named FIFO (for tmux pipe-pane).
func (f *EncryptingFIFO) FIFOPath() string {
	return f.fifoPath
}

// Close stops the reader goroutine, closes the writer, and removes the FIFO.
func (f *EncryptingFIFO) Close() error {
	close(f.stopCh)
	// Give the reader a moment to drain
	select {
	case <-f.doneCh:
	case <-time.After(2 * time.Second):
	}
	f.writer.Close()
	os.Remove(f.fifoPath)
	return nil
}

func (f *EncryptingFIFO) readLoop() {
	defer close(f.doneCh)

	for {
		select {
		case <-f.stopCh:
			return
		default:
		}

		// Open FIFO for reading (blocks until a writer connects)
		// O_RDONLY on a FIFO blocks until a writer opens it.
		// When the writer (tmux cat) closes, Read returns EOF and we reopen.
		file, err := os.OpenFile(f.fifoPath, os.O_RDONLY, 0)
		if err != nil {
			select {
			case <-f.stopCh:
				return
			default:
				time.Sleep(100 * time.Millisecond)
				continue
			}
		}

		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 64*1024)
		for scanner.Scan() {
			select {
			case <-f.stopCh:
				file.Close()
				return
			default:
			}
			line := scanner.Bytes()
			line = append(line, '\n')
			f.writer.Write(line)
		}
		file.Close()

		// Flush after each writer disconnect (tmux session ends or restarts)
		f.writer.Flush()
	}
}
