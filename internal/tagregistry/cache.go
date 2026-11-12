package tagregistry

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// FileCache keeps the registry copy in one file, such as a file in the
// autonomous data directory. Save writes a temporary file beside the copy and
// renames it over the old one, so a crash leaves the old copy or the new one,
// never a partial file.
type FileCache struct {
	Path string
}

// Load reads the saved copy. A missing file is not an error: ok is false.
func (c FileCache) Load() (data []byte, ok bool, err error) {
	data, err = os.ReadFile(c.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

// Save replaces the saved copy, creating the directory when it is missing.
func (c FileCache) Save(data []byte) error {
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}
