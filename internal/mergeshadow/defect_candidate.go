package mergeshadow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// defectCandidateDir is the subdirectory of the daemon data directory that
// holds defect candidates, one JSON file per Automaton id.
const defectCandidateDir = "defect-candidates"

// StatusCandidate is the status every new DefectCandidate carries. It marks
// the difference as a candidate for the defect queue. Filing a candidate into
// the queue is the job of the existing defect intake, not of this package.
const StatusCandidate = "candidate"

// DefectCandidate is one Difference from a Comparison, kept as a record that
// a defect may need filing. Key is the stable identity of the candidate: two
// candidates with the same key are the same candidate.
type DefectCandidate struct {
	Key         string      `json:"key"`
	AutomatonID string      `json:"automaton_id"`
	Field       string      `json:"field"`
	Planned     string      `json:"planned"`
	Landed      string      `json:"landed"`
	Severity    Severity    `json:"severity"`
	ReleaseSize ReleaseSize `json:"release_size"`
	Status      string      `json:"status"`
	CreatedAt   time.Time   `json:"created_at"`
}

// DefectCandidateKey returns the stable key of a candidate: a SHA-256 hex
// digest over the Automaton id, the field, the planned and landed values, the
// release size, the status and created_at. The parts are JSON-encoded before
// hashing so that no two different inputs can share an encoding. created_at is
// normalised to UTC with nanosecond precision.
func DefectCandidateKey(automatonID, field, planned, landed string, size ReleaseSize, status string, createdAt time.Time) string {
	parts := []string{
		automatonID,
		field,
		planned,
		landed,
		string(size),
		status,
		createdAt.UTC().Format(time.RFC3339Nano),
	}
	data, _ := json.Marshal(parts) // marshalling a []string cannot fail
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// DefectCandidatesFromComparison turns every Difference in c into a
// DefectCandidate with status StatusCandidate. A comparison with no
// differences yields an empty, non-nil slice. The result is in the order of
// c.Differences.
func DefectCandidatesFromComparison(c *Comparison) ([]DefectCandidate, error) {
	if c == nil {
		return nil, errors.New("mergeshadow: nil comparison")
	}
	out := make([]DefectCandidate, 0, len(c.Differences))
	for _, d := range c.Differences {
		out = append(out, DefectCandidate{
			Key:         DefectCandidateKey(c.AutomatonID, d.Field, d.Planned, d.Landed, c.ReleaseSize, StatusCandidate, c.CreatedAt),
			AutomatonID: c.AutomatonID,
			Field:       d.Field,
			Planned:     d.Planned,
			Landed:      d.Landed,
			Severity:    d.Severity,
			ReleaseSize: c.ReleaseSize,
			Status:      StatusCandidate,
			CreatedAt:   c.CreatedAt,
		})
	}
	return out, nil
}

// RecordComparisonCandidates derives the candidates of c and persists them to
// <dataDir>/defect-candidates/<automaton id>.json. A candidate whose key is
// already stored is not written again. The returned slice holds only the
// candidates created by this call; it is empty, not nil, when none were new.
// Nothing is written when no new candidate exists.
//
// Keys include created_at, so running Compare again produces a new comparison
// timestamp and therefore new keys. Only a repeat record of the same
// comparison, with the same created_at, is a duplicate.
func RecordComparisonCandidates(dataDir string, c *Comparison) ([]DefectCandidate, error) {
	derived, err := DefectCandidatesFromComparison(c)
	if err != nil {
		return nil, err
	}
	p, err := storePath(dataDir, defectCandidateDir, c.AutomatonID)
	if err != nil {
		return nil, err
	}

	storeMu.Lock()
	defer storeMu.Unlock()

	stored, err := readCandidateFile(p)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(stored)+len(derived))
	for _, s := range stored {
		seen[s.Key] = true
	}
	created := []DefectCandidate{}
	for _, cand := range derived {
		if seen[cand.Key] {
			continue
		}
		seen[cand.Key] = true
		stored = append(stored, cand)
		created = append(created, cand)
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

// ListDefectCandidates returns the stored candidates for one Automaton, newest
// first by CreatedAt, ties broken by key. The result is never nil.
func ListDefectCandidates(dataDir, automatonID string) ([]DefectCandidate, error) {
	p, err := storePath(dataDir, defectCandidateDir, automatonID)
	if err != nil {
		return nil, err
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	cands, err := readCandidateFile(p)
	if err != nil {
		return nil, err
	}
	sortCandidates(cands)
	return cands, nil
}

// ListAllDefectCandidates returns the stored candidates of every Automaton,
// newest first by CreatedAt, ties broken by key. The result is never nil.
func ListAllDefectCandidates(dataDir string) ([]DefectCandidate, error) {
	if dataDir == "" {
		return nil, errors.New("mergeshadow: empty data directory")
	}
	dir := filepath.Join(dataDir, defectCandidateDir)

	storeMu.Lock()
	defer storeMu.Unlock()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []DefectCandidate{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []DefectCandidate{}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasPrefix(name, ".") {
			continue
		}
		cands, err := readCandidateFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		out = append(out, cands...)
	}
	sortCandidates(out)
	return out, nil
}

// readCandidateFile reads one automaton's candidate file. A missing file is
// no candidates. The caller holds storeMu.
func readCandidateFile(p string) ([]DefectCandidate, error) {
	data, err := os.ReadFile(p) // #nosec G304 -- id validated by storePath, path under dataDir
	if errors.Is(err, os.ErrNotExist) {
		return []DefectCandidate{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cands []DefectCandidate
	if err := json.Unmarshal(data, &cands); err != nil {
		return nil, fmt.Errorf("mergeshadow: parse %s: %w", p, err)
	}
	if cands == nil {
		cands = []DefectCandidate{}
	}
	return cands, nil
}

// sortCandidates orders candidates newest first by CreatedAt, ties broken by key.
func sortCandidates(cands []DefectCandidate) {
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.Key < b.Key
	})
}
