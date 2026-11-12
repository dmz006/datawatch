package mergeshadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// compareStoreDir is the subdirectory of the daemon data directory that
// holds comparisons.
const compareStoreDir = "merge-comparisons"

// storePath builds and validates the store file path for one automaton id
// inside the given subdirectory. The id must be a single plain name: no
// empty ids, no "." / "..", no path separators.
func storePath(dataDir, sub, id string) (string, error) {
	if !validStoreID.MatchString(id) {
		return "", fmt.Errorf("mergeshadow: invalid automaton id %q", id)
	}
	if dataDir == "" {
		return "", errors.New("mergeshadow: empty data directory")
	}
	dir := filepath.Join(dataDir, sub)
	return containedPath(dir, filepath.Base(id)+".json")
}

// validStoreID is the strict allowlist for an automaton id used as a file
// name: letters, digits, dash and underscore only. Path separators, dots and
// the empty string never match.
var validStoreID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// containedPath joins name to dir and verifies the cleaned result is still
// directly inside dir. name must be a single plain file name. It is the one
// check every store read, write and remove goes through.
func containedPath(dir, name string) (string, error) {
	if name == "" || name != filepath.Base(name) || name == "." || name == ".." {
		return "", fmt.Errorf("mergeshadow: invalid store file name %q", name)
	}
	base := filepath.Clean(dir)
	p := filepath.Join(base, name)
	rel, err := filepath.Rel(base, p)
	if err != nil || rel != name || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("mergeshadow: path %q escapes the store directory", p)
	}
	return p, nil
}

// SaveComparison writes the comparison to <dataDir>/merge-comparisons/<id>.json.
// The comparison's AutomatonID must match id.
//
// The write is atomic in the same sense as Save: a temp file is created
// inside the destination directory, synced, and renamed over the target.
// A pre-existing comparison for the id is replaced.
func SaveComparison(dataDir, id string, c *Comparison) error {
	p, err := storePath(dataDir, compareStoreDir, id)
	if err != nil {
		return err
	}
	if c == nil {
		return errors.New("mergeshadow: nil comparison")
	}
	if c.AutomatonID != id {
		return fmt.Errorf("mergeshadow: comparison automaton_id %q does not match id %q", c.AutomatonID, id)
	}
	out := *c
	if out.Differences == nil {
		out.Differences = []Difference{}
	}
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return err
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	return writeAtomic(filepath.Dir(p), filepath.Base(p), data)
}

// LoadComparison reads <dataDir>/merge-comparisons/<id>.json.
func LoadComparison(dataDir, id string) (*Comparison, error) {
	p, err := storePath(dataDir, compareStoreDir, id)
	if err != nil {
		return nil, err
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	return readComparison(p)
}

func readComparison(p string) (*Comparison, error) {
	data, err := os.ReadFile(p) // #nosec G304 -- id validated, path under dataDir
	if err != nil {
		return nil, err
	}
	var c Comparison
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("mergeshadow: parse %s: %w", p, err)
	}
	return &c, nil
}

// ListComparisons returns all stored comparisons, newest first by
// CreatedAt, ties broken by automaton id. The result is never nil.
func ListComparisons(dataDir string) ([]*Comparison, error) {
	if dataDir == "" {
		return nil, errors.New("mergeshadow: empty data directory")
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	return listComparisonsLocked(filepath.Join(dataDir, compareStoreDir))
}

func listComparisonsLocked(dir string) ([]*Comparison, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []*Comparison{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []*Comparison{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		c, err := readComparison(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.AutomatonID < b.AutomatonID
	})
	return out, nil
}
