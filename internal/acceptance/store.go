package acceptance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/dmz006/datawatch/internal/secfile"
)

// StartRecord is the first notice, spawn or typed input of a request. The
// request sources record it for the standing check.
type StartRecord struct {
	RequestID string    `json:"request_id"`
	Source    string    `json:"source"`
	At        time.Time `json:"at"`
}

type data struct {
	Version int `json:"version"`
	// StartMark is when report mode was first on; the standing check looks at
	// requests started after it.
	StartMark time.Time          `json:"start_mark,omitempty"`
	Lists     map[string]*List   `json:"lists"`
	Verdicts  map[string]Verdict `json:"verdicts"`
	Starts    []StartRecord      `json:"starts,omitempty"`
}

func newData() data {
	return data{Version: 1, Lists: map[string]*List{}, Verdicts: map[string]Verdict{}}
}

// loadData reads the store. A file that cannot be read or decoded is an
// error: it is never treated as empty and never written over.
func loadData(path string, key []byte) (data, error) {
	d := newData()
	raw, err := os.ReadFile(path) // #nosec G304 -- the daemon's own data file
	if errors.Is(err, fs.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("acceptance store: %w", err)
	}
	if secfile.IsEncrypted(raw) {
		if key == nil {
			return d, errors.New("acceptance store: the file is encrypted and there is no key")
		}
		if raw, err = secfile.Decrypt(raw, key); err != nil {
			return d, fmt.Errorf("acceptance store: %w", err)
		}
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("acceptance store: %w", err)
	}
	if d.Lists == nil {
		d.Lists = map[string]*List{}
	}
	if d.Verdicts == nil {
		d.Verdicts = map[string]Verdict{}
	}
	return d, nil
}

// saveData replaces the store file in one step, encrypted when there is a key.
func saveData(path string, key []byte, d data) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("acceptance store: %w", err)
	}
	if key != nil {
		if raw, err = secfile.Encrypt(raw, key); err != nil {
			return fmt.Errorf("acceptance store: %w", err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("acceptance store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".acceptance-*")
	if err != nil {
		return fmt.Errorf("acceptance store: %w", err)
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	_, err = tmp.Write(raw)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("acceptance store: %w", err)
	}
	return nil
}

// clone returns a deep copy through the stored form.
func clone[T any](v T) (T, error) {
	var out T
	raw, err := json.Marshal(v)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}
