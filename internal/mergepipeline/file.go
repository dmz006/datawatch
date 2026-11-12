package mergepipeline

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// DefaultMaxFileBytes caps the size of a pipeline file.
const DefaultMaxFileBytes int64 = 64 * 1024

// ErrNotFound means the project has no pipeline file.
type ErrNotFound struct{ Path string }

func (e *ErrNotFound) Error() string { return "merge pipeline file not found: " + e.Path }

// ErrMalformed means the file exists but cannot be used: bad YAML, unknown
// fields, or a definition that fails validation.
type ErrMalformed struct {
	Path string
	Err  error
}

func (e *ErrMalformed) Error() string {
	return fmt.Sprintf("merge pipeline file %s is malformed: %v", e.Path, e.Err)
}
func (e *ErrMalformed) Unwrap() error { return e.Err }

// ErrTooLarge means the file is bigger than the size cap.
type ErrTooLarge struct {
	Path  string
	Limit int64
}

func (e *ErrTooLarge) Error() string {
	return fmt.Sprintf("merge pipeline file %s is larger than %d bytes", e.Path, e.Limit)
}

// LoadFile reads the pipeline named fileName (relative to projectDir) from a
// project's repository. fileName is a parameter so callers choose it, for
// example from a setting. maxBytes <= 0 uses DefaultMaxFileBytes. The result
// is validated.
func LoadFile(projectDir, fileName string, maxBytes int64) (Definition, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}
	if fileName == "" {
		return Definition{}, &ErrMalformed{Path: fileName, Err: errors.New("no file name given")}
	}
	if err := checkDir(fileName); err != nil {
		return Definition{}, &ErrMalformed{Path: fileName, Err: err}
	}
	full := filepath.Join(projectDir, filepath.FromSlash(fileName))
	// Open the file through a root handle on the project directory: a name that
	// resolves outside the project (a "..", an absolute path or a symlink that
	// leaves it) is refused by the operating system, not only by the name check.
	root, err := os.OpenRoot(projectDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Definition{}, &ErrNotFound{Path: full}
		}
		return Definition{}, &ErrMalformed{Path: full, Err: err}
	}
	defer func() { _ = root.Close() }()
	f, err := root.Open(filepath.FromSlash(fileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Definition{}, &ErrNotFound{Path: full}
		}
		return Definition{}, &ErrMalformed{Path: full, Err: err}
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return Definition{}, &ErrMalformed{Path: full, Err: err}
	}
	if int64(len(data)) > maxBytes {
		return Definition{}, &ErrTooLarge{Path: full, Limit: maxBytes}
	}
	return Parse(data, full)
}

// Parse decodes and validates pipeline YAML. name is used in errors.
func Parse(data []byte, name string) (Definition, error) {
	var d Definition
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&d); err != nil {
		return Definition{}, &ErrMalformed{Path: name, Err: err}
	}
	if err := d.Validate(); err != nil {
		return Definition{}, &ErrMalformed{Path: name, Err: err}
	}
	return d, nil
}

// Marshal encodes a definition as YAML.
func Marshal(d Definition) ([]byte, error) { return yaml.Marshal(d) }
