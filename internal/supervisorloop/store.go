package supervisorloop

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// FileStore appends run records as JSON lines to a caller-supplied path and
// keeps only the newest Retention entries.
type FileStore struct {
	path      string
	retention int
	mu        sync.Mutex
}

// NewFileStore returns a store writing to path (never hard-coded here).
func NewFileStore(path string, retention int) *FileStore {
	return &FileStore{path: path, retention: retention}
}

// Append writes a record and trims the file.
func (f *FileStore) Append(r RunRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	fh, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := fh.Write(append(line, '\n'))
	if cerr := fh.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	return f.trim()
}

func (f *FileStore) read() ([]RunRecord, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []RunRecord
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var r RunRecord
		if json.Unmarshal(sc.Bytes(), &r) == nil {
			out = append(out, r)
		}
	}
	return out, sc.Err()
}

func (f *FileStore) trim() error {
	recs, err := f.read()
	if err != nil || f.retention < 1 || len(recs) <= f.retention {
		return err
	}
	recs = recs[len(recs)-f.retention:]
	var buf bytes.Buffer
	for _, r := range recs {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

// List returns the stored records, oldest first.
func (f *FileStore) List() ([]RunRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read()
}
