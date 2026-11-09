package mergepipeline

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ConflictCounter counts merge conflicts per project. The counts are kept in a
// single JSON file at <dataDir>/merge-pipeline/conflict-counter.json, written to
// a temporary file and renamed so a crash never leaves a half-written file.
// It is safe for concurrent use.
//
// A corrupt counter file is reported as an error by NewConflictCounter; it is
// never silently reset to zero, because that would hide the lost history.
type ConflictCounter struct {
	mu     sync.Mutex
	path   string
	counts map[string]int
}

// NewConflictCounter loads (or starts) the counter kept at
// <dataDir>/merge-pipeline/conflict-counter.json.
func NewConflictCounter(dataDir string) (*ConflictCounter, error) {
	c := &ConflictCounter{
		path:   filepath.Join(dataDir, "merge-pipeline", "conflict-counter.json"),
		counts: map[string]int{},
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// Count returns the number of conflicts recorded for the project. A project
// with no recorded conflicts has a count of zero.
func (c *ConflictCounter) Count(project string) (int, error) {
	if strings.TrimSpace(project) == "" {
		return 0, errors.New("project name is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[project], nil
}

// Increment records one more conflict for the project and returns the new
// count. If the change cannot be persisted, it is rolled back and the error is
// returned.
func (c *ConflictCounter) Increment(project string) (int, error) {
	if strings.TrimSpace(project) == "" {
		return 0, errors.New("project name is required")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	prev, had := c.counts[project]
	next := prev + 1
	c.counts[project] = next
	if err := c.persist(); err != nil {
		if had {
			c.counts[project] = prev
		} else {
			delete(c.counts, project)
		}
		return 0, err
	}
	return next, nil
}

func (c *ConflictCounter) persist() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.counts, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func (c *ConflictCounter) load() error {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var m map[string]int
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("corrupt conflict counter file %s: %w", c.path, err)
	}
	for project, n := range m {
		if n < 0 {
			return fmt.Errorf("corrupt conflict counter file %s: negative count %d for project %q", c.path, n, project)
		}
		c.counts[project] = n
	}
	return nil
}
