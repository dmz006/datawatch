package mergeshadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// storeDir is the subdirectory of the daemon data directory holding plans.
const storeDir = "merge-plans"

// storeMu serialises every store operation in this process, so concurrent
// Save calls (including for the same Automaton id) and Prune/List never
// interleave. Each Save is also atomic on disk (temp file + rename), so a
// reader never sees a half-written plan even without the lock.
var storeMu sync.Mutex

func planPath(dataDir, id string) (string, error) {
	return storePath(dataDir, storeDir, id)
}

// Save writes the plan to <dataDir>/merge-plans/<id>.json. The plan's
// AutomatonID must equal id and Applied must be false. The file is written to
// a temporary file in the same directory, synced, and renamed over the target,
// so a crash leaves either the old file or the new one, never a partial file.
func Save(dataDir, id string, plan *MergePlan) error {
	p, err := planPath(dataDir, id)
	if err != nil {
		return err
	}
	if plan == nil {
		return errors.New("mergeshadow: nil plan")
	}
	if plan.AutomatonID != id {
		return fmt.Errorf("mergeshadow: plan automaton_id %q does not match id %q", plan.AutomatonID, id)
	}
	if plan.Applied {
		return errors.New("mergeshadow: plan must not be marked applied")
	}
	// Serialise a copy so nil lists are written as [] and the caller's
	// value is left untouched.
	out := *plan
	if out.Conflicts == nil {
		out.Conflicts = []Conflict{}
	}
	if out.Gates == nil {
		out.Gates = []Gate{}
	}
	if out.Findings == nil {
		out.Findings = []Finding{}
	}
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return err
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	return writeAtomic(filepath.Dir(p), filepath.Base(p), data)
}

// writeAtomic writes data to dir/name via a synced temp file and a rename.
func writeAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".plan-*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}

// Load reads <dataDir>/merge-plans/<id>.json.
func Load(dataDir, id string) (*MergePlan, error) {
	p, err := planPath(dataDir, id)
	if err != nil {
		return nil, err
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	return readPlan(p)
}

func readPlan(p string) (*MergePlan, error) {
	data, err := os.ReadFile(p) // #nosec G304 -- id validated, path under dataDir
	if err != nil {
		return nil, err
	}
	var plan MergePlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("mergeshadow: parse %s: %w", p, err)
	}
	return &plan, nil
}

// List returns every stored plan, newest first by CreatedAt (ties broken by
// automaton id). Temporary files left by an interrupted Save are skipped.
func List(dataDir string) ([]*MergePlan, error) {
	if dataDir == "" {
		return nil, errors.New("mergeshadow: empty data directory")
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	stored, err := listLocked(filepath.Join(dataDir, storeDir))
	if err != nil {
		return nil, err
	}
	sortNewestFirst(stored)
	plans := make([]*MergePlan, 0, len(stored))
	for _, s := range stored {
		plans = append(plans, s.plan)
	}
	return plans, nil
}

// storedPlan is a plan together with the file it was read from.
type storedPlan struct {
	plan *MergePlan
	path string
}

func listLocked(dir string) ([]storedPlan, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []storedPlan{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []storedPlan{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		p := filepath.Join(dir, name)
		plan, err := readPlan(p)
		if err != nil {
			return nil, err
		}
		out = append(out, storedPlan{plan: plan, path: p})
	}
	return out, nil
}

func sortNewestFirst(plans []storedPlan) {
	sort.Slice(plans, func(i, j int) bool {
		a, b := plans[i].plan, plans[j].plan
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.AutomatonID < b.AutomatonID
	})
}

// Prune deletes, for each project, every plan beyond the newest keep plans
// (by CreatedAt). It returns how many plans were removed. keep must be at
// least 1, so a bad call cannot empty the store.
func Prune(dataDir string, keep int) (int, error) {
	if dataDir == "" {
		return 0, errors.New("mergeshadow: empty data directory")
	}
	if keep < 1 {
		return 0, fmt.Errorf("mergeshadow: keep must be at least 1, got %d", keep)
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	plans, err := listLocked(filepath.Join(dataDir, storeDir))
	if err != nil {
		return 0, err
	}
	sortNewestFirst(plans) // newest first, so the first keep per project survive
	seen := map[string]int{}
	removed := 0
	for _, s := range plans {
		seen[s.plan.Project]++
		if seen[s.plan.Project] <= keep {
			continue
		}
		if err := os.Remove(s.path); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
