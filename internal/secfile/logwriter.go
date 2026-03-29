package secfile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Streaming encrypted log file format:
//
//	"DWLOG1\n"
//	[block_len_u32_le][nonce12 + ciphertext]
//	[block_len_u32_le][nonce12 + ciphertext]
//	...
//
// Each block is independently decryptable with a fresh nonce.
// Blocks are flushed every flushSize bytes or on Close.

const (
	logMagic  = "DWLOG1\n"
	flushSize = 4096 // flush encrypted block every 4KB of plaintext
)

// IsEncryptedLog reports whether data starts with the encrypted log magic header.
func IsEncryptedLog(data []byte) bool {
	return strings.HasPrefix(string(data), logMagic)
}

// EncryptedLogWriter writes AES-256-GCM encrypted blocks to a log file.
// Each block is independently decryptable. Thread-safe.
type EncryptedLogWriter struct {
	mu     sync.Mutex
	f      *os.File
	gcm    cipher.AEAD
	buf    []byte
	closed bool
}

// NewEncryptedLogWriter creates a new encrypted log file at path.
// Writes the magic header immediately. The key must be 32 bytes.
func NewEncryptedLogWriter(path string, key []byte) (*EncryptedLogWriter, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secfile: key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return nil, err
	}
	if _, err := f.WriteString(logMagic); err != nil {
		f.Close()
		return nil, err
	}
	return &EncryptedLogWriter{f: f, gcm: gcm}, nil
}

// Write appends data to the buffer and flushes encrypted blocks as needed.
func (w *EncryptedLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0, fmt.Errorf("secfile: writer closed")
	}
	w.buf = append(w.buf, p...)
	for len(w.buf) >= flushSize {
		if err := w.flushBlock(w.buf[:flushSize]); err != nil {
			return 0, err
		}
		w.buf = w.buf[flushSize:]
	}
	return len(p), nil
}

// Close flushes any remaining buffered data and closes the file.
func (w *EncryptedLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	if len(w.buf) > 0 {
		if err := w.flushBlock(w.buf); err != nil {
			w.f.Close()
			return err
		}
	}
	return w.f.Close()
}

// Flush forces any buffered data to be written as an encrypted block.
func (w *EncryptedLogWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) == 0 {
		return nil
	}
	err := w.flushBlock(w.buf)
	w.buf = w.buf[:0]
	return err
}

func (w *EncryptedLogWriter) flushBlock(data []byte) error {
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ct := w.gcm.Seal(nonce, nonce, data, nil) // nonce || ciphertext

	// Write length-prefixed block: [u32le length][encrypted data]
	var lenBuf [4]byte
	binary.LittleEndian.PutUint32(lenBuf[:], uint32(len(ct)))
	if _, err := w.f.Write(lenBuf[:]); err != nil {
		return err
	}
	if _, err := w.f.Write(ct); err != nil {
		return err
	}
	return nil
}

// EncryptedLogReader reads and decrypts blocks from an encrypted log file.
type EncryptedLogReader struct {
	f   *os.File
	gcm cipher.AEAD
}

// NewEncryptedLogReader opens an encrypted log file for reading.
func NewEncryptedLogReader(path string, key []byte) (*EncryptedLogReader, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("secfile: key must be 32 bytes, got %d", len(key))
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	// Read and verify magic header
	header := make([]byte, len(logMagic))
	if _, err := io.ReadFull(f, header); err != nil {
		f.Close()
		return nil, fmt.Errorf("secfile: read header: %w", err)
	}
	if string(header) != logMagic {
		f.Close()
		return nil, fmt.Errorf("secfile: not an encrypted log file")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		f.Close()
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		f.Close()
		return nil, err
	}
	return &EncryptedLogReader{f: f, gcm: gcm}, nil
}

// ReadAll decrypts all blocks and returns the concatenated plaintext.
func (r *EncryptedLogReader) ReadAll() ([]byte, error) {
	var result []byte
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r.f, lenBuf[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return nil, err
		}
		blockLen := binary.LittleEndian.Uint32(lenBuf[:])
		if blockLen > 10*1024*1024 { // 10MB sanity check
			return nil, fmt.Errorf("secfile: block too large (%d bytes)", blockLen)
		}
		block := make([]byte, blockLen)
		if _, err := io.ReadFull(r.f, block); err != nil {
			return nil, fmt.Errorf("secfile: read block: %w", err)
		}
		if len(block) < nonceLen+1 {
			return nil, fmt.Errorf("secfile: block too short")
		}
		nonce := block[:nonceLen]
		ct := block[nonceLen:]
		pt, err := r.gcm.Open(nil, nonce, ct, nil)
		if err != nil {
			return nil, fmt.Errorf("secfile: decrypt block: %w", err)
		}
		result = append(result, pt...)
	}
	return result, nil
}

// Close closes the underlying file.
func (r *EncryptedLogReader) Close() error {
	return r.f.Close()
}
