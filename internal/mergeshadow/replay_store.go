package mergeshadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	// replayStoreDir holds the replays, handCheckStoreDir the hand merges'
	// recorded check results. They are two files so a result recorded while
	// the shadow still runs is never overwritten by it.
	replayStoreDir    = "merge-shadow-replays"
	handCheckStoreDir = "merge-shadow-hand-checks"
)

// The results a hand check can have.
const (
	HandCheckPass = "pass"
	HandCheckFail = "fail"
	HandCheckSkip = "skip"
)

var handCheckName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_. -]{0,79}$`)

// maxHandChecks bounds one hand merge's recorded results.
const maxHandChecks = 100

// ReplayKey names the replay of one Automaton's hand merge.
func ReplayKey(automatonID, handCommit string) string {
	return automatonID + "-" + shortCommit(handCommit)
}

// ValidReplayKey reports whether key may name a stored replay, and so a
// folder or file of its own.
func ValidReplayKey(key string) bool { return validStoreID.MatchString(key) }

// ValidateHandChecks checks recorded hand results: each names a pipeline check
// step and says pass, fail or skip.
func ValidateHandChecks(checks map[string]string) error {
	if len(checks) > maxHandChecks {
		return fmt.Errorf("hand checks: at most %d", maxHandChecks)
	}
	for name, result := range checks {
		if !handCheckName.MatchString(name) {
			return fmt.Errorf("hand checks: %q is not a check name", name)
		}
		switch result {
		case HandCheckPass, HandCheckFail, HandCheckSkip:
		default:
			return fmt.Errorf("hand checks: the result of %q must be pass, fail or skip", name)
		}
	}
	return nil
}

// ParseHandChecks reads "name=result" pairs, one per entry or separated by
// commas.
func ParseHandChecks(pairs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, entry := range pairs {
		for _, pair := range strings.Split(entry, ",") {
			if strings.TrimSpace(pair) == "" {
				continue
			}
			name, result, ok := strings.Cut(pair, "=")
			name, result = strings.TrimSpace(name), strings.ToLower(strings.TrimSpace(result))
			if !ok {
				return nil, fmt.Errorf("hand checks: %q is not name=result", strings.TrimSpace(pair))
			}
			if prev, seen := out[name]; seen && prev != result {
				return nil, fmt.Errorf("hand checks: %q is given twice with different results", name)
			}
			out[name] = result
		}
	}
	return out, ValidateHandChecks(out)
}

func saveJSON(dataDir, sub, id string, v any) error {
	p, err := storePath(dataDir, sub, id)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	return writeAtomic(filepath.Dir(p), filepath.Base(p), data)
}

func loadJSON(dataDir, sub, id string, v any) error {
	p, err := storePath(dataDir, sub, id)
	if err != nil {
		return err
	}
	storeMu.Lock()
	defer storeMu.Unlock()
	data, err := os.ReadFile(p) // #nosec G304 -- id validated, path under dataDir
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("mergeshadow: parse %s: %w", p, err)
	}
	return nil
}

// SaveReplay stores a replay under its key. The hand checks are not part of
// the file.
func SaveReplay(dataDir string, r *Replay) error {
	if r == nil {
		return errors.New("mergeshadow: nil replay")
	}
	out := *r
	out.HandChecks = nil
	return saveJSON(dataDir, replayStoreDir, r.Key, &out)
}

// SaveHandChecks records the hand merge's check results for the replay key,
// replacing earlier ones.
func SaveHandChecks(dataDir, key string, checks map[string]string) error {
	if err := ValidateHandChecks(checks); err != nil {
		return err
	}
	return saveJSON(dataDir, handCheckStoreDir, key, checks)
}

func fillHandChecks(dataDir string, r *Replay) error {
	checks := map[string]string{}
	err := loadJSON(dataDir, handCheckStoreDir, r.Key, &checks)
	if errors.Is(err, os.ErrNotExist) {
		r.HandChecks = nil
		return nil
	}
	if err != nil {
		return err
	}
	r.HandChecks = checks
	return nil
}

// LoadReplay reads one replay with its recorded hand checks.
func LoadReplay(dataDir, key string) (*Replay, error) {
	var r Replay
	if err := loadJSON(dataDir, replayStoreDir, key, &r); err != nil {
		return nil, err
	}
	return &r, fillHandChecks(dataDir, &r)
}

// ListReplays returns every replay, newest hand merge first; never nil.
func ListReplays(dataDir string) ([]*Replay, error) {
	if dataDir == "" {
		return nil, errors.New("mergeshadow: empty data directory")
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, replayStoreDir))
	if errors.Is(err, os.ErrNotExist) {
		return []*Replay{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []*Replay{}
	for _, e := range entries {
		key, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || e.IsDir() || !validStoreID.MatchString(key) {
			continue
		}
		r, err := LoadReplay(dataDir, key)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	SortReplays(out)
	return out, nil
}
