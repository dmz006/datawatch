package mergeshadow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// intermittentDefectDir is the subdirectory of the daemon data directory that
// holds intermittent-gate defect records, one JSON file per Automaton id.
const intermittentDefectDir = "intermittent-defects"

// StatusIntermittentDefect is the status every new IntermittentDefect carries:
// it marks the record as a defect that nobody has fixed yet.
const StatusIntermittentDefect = "open"

// unnamedFailingTest is the test name of a record whose first run output did
// not name the failing test (the output was cut off or was not go test output).
const unnamedFailingTest = "test gate (failing test not named in the output)"

// IntermittentDefect is the defect record made when the merge shadow reports
// a gate as intermittent: it failed on the first run and passed on the rerun.
// The failure is a defect in a test or in the code, so it is recorded and
// shown on the Housekeeping card; it never blocks the merge. Key is the stable
// identity: the same test at the same commit is the same record.
type IntermittentDefect struct {
	Key         string `json:"key"`
	AutomatonID string `json:"automaton_id"`
	// Test is the failing test name parsed from the first run's output.
	Test string `json:"test"`
	// Message is the failure message: the first assertion line of the test.
	Message string `json:"message"`
	// FirstLines are the leading lines of the first run's output (the same
	// lines the gate keeps as its finding).
	FirstLines string `json:"first_lines"`
	// Date is the day the record was made, as YYYY-MM-DD in UTC.
	Date string `json:"date"`
	// Commit is the commit under test: the head of the work branch.
	Commit    string    `json:"commit"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// IntermittentDefectKey returns the stable key of an intermittent-gate defect:
// a SHA-256 hex digest over the test name and the commit under test. The date,
// the message and the Automaton id are not part of it, so the same test at the
// same commit never files twice. The parts are JSON-encoded before hashing so
// no two different inputs share an encoding.
func IntermittentDefectKey(test, commit string) string {
	data, _ := json.Marshal([]string{"intermittent", test, commit}) // a []string cannot fail to marshal
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// plainFailRe matches the `--- FAIL: TestName (0.00s)` line of plain (not
// JSON) go test output.
var plainFailRe = regexp.MustCompile(`(?m)^\s*--- FAIL: (\S+)`)

// failingTestNames lists the failing tests of a first run: from go test JSON
// output when it holds failures, else from plain `--- FAIL:` lines. The
// message is the first assertion line (JSON output only; for plain output it
// is the first non-bookkeeping line after the FAIL line).
func failingTestNames(output string) (names []string, message string) {
	names, message = ParseGoTestFailures(output)
	if len(names) > 0 {
		return names, message
	}
	seen := map[string]bool{}
	for _, m := range plainFailRe.FindAllStringSubmatch(output, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			names = append(names, m[1])
		}
	}
	if len(names) > 0 {
		message = assertionLine(strings.Split(output, "\n"))
	}
	return names, message
}

// IntermittentDefectsFromGate builds the defect records of a gate result. A
// result that is not GateIntermittent yields none: a passing gate and a gate
// that failed on the rerun too (a persistent failure, already blocking) are not
// intermittent. One record is made per failing test named in the first run's
// output, or one record with a generic name when no test is named. commit is
// the commit under test and now the time of the record; both come from the
// caller so the result is deterministic.
func IntermittentDefectsFromGate(res GateResult, automatonID, commit string, now time.Time) []IntermittentDefect {
	if res.Status != GateIntermittent {
		return nil
	}
	names, message := failingTestNames(res.Output)
	if len(names) == 0 {
		names = []string{unnamedFailingTest}
	}
	if message == "" {
		message = assertionLine(strings.Split(res.Output, "\n"))
	}
	first := res.Finding
	if first == "" {
		first = leadingLines(res.Output, findingLines)
	}
	now = now.UTC()
	out := make([]IntermittentDefect, 0, len(names))
	for _, n := range names {
		out = append(out, IntermittentDefect{
			Key:         IntermittentDefectKey(n, commit),
			AutomatonID: automatonID,
			Test:        n,
			Message:     message,
			FirstLines:  first,
			Date:        now.Format(dateLayout),
			Commit:      commit,
			Status:      StatusIntermittentDefect,
			CreatedAt:   now,
		})
	}
	return out
}

// RecordIntermittentDefects persists the defects to
// <dataDir>/intermittent-defects/<automaton id>.json and returns only the ones
// created by this call. A defect whose key is already stored, for any
// Automaton, is not stored again. Nothing is written when none is new.
func RecordIntermittentDefects(dataDir, automatonID string, defects []IntermittentDefect) ([]IntermittentDefect, error) {
	created := []IntermittentDefect{}
	if len(defects) == 0 {
		return created, nil
	}
	p, err := storePath(dataDir, intermittentDefectDir, automatonID)
	if err != nil {
		return nil, err
	}

	storeMu.Lock()
	defer storeMu.Unlock()

	all, err := readAllIntermittentLocked(dataDir)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(all)+len(defects))
	for _, d := range all {
		known[d.Key] = true
	}
	stored, err := readIntermittentFile(p)
	if err != nil {
		return nil, err
	}
	for _, d := range defects {
		if known[d.Key] {
			continue
		}
		known[d.Key] = true
		stored = append(stored, d)
		created = append(created, d)
	}
	if len(created) == 0 {
		return created, nil
	}
	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(filepath.Dir(p), filepath.Base(p), data); err != nil {
		return nil, err
	}
	return created, nil
}

// ListIntermittentDefects returns the stored intermittent-gate defects of every
// Automaton, newest first by CreatedAt, ties broken by key. Never nil.
func ListIntermittentDefects(dataDir string) ([]IntermittentDefect, error) {
	if dataDir == "" {
		return nil, errors.New("mergeshadow: empty data directory")
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	return readAllIntermittentLocked(dataDir)
}

// readAllIntermittentLocked reads every Automaton's file. The caller holds storeMu.
func readAllIntermittentLocked(dataDir string) ([]IntermittentDefect, error) {
	dir := filepath.Join(dataDir, intermittentDefectDir)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []IntermittentDefect{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []IntermittentDefect{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		ds, err := readIntermittentFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, ds...)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.Key < b.Key
	})
	return out, nil
}

// readIntermittentFile reads one Automaton's file. A missing file is no
// defects. The caller holds storeMu.
func readIntermittentFile(p string) ([]IntermittentDefect, error) {
	data, err := os.ReadFile(p) // #nosec G304 -- id validated by storePath or listed from the store directory
	if errors.Is(err, os.ErrNotExist) {
		return []IntermittentDefect{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ds []IntermittentDefect
	if err := json.Unmarshal(data, &ds); err != nil {
		return nil, fmt.Errorf("mergeshadow: parse %s: %w", p, err)
	}
	if ds == nil {
		ds = []IntermittentDefect{}
	}
	return ds, nil
}
