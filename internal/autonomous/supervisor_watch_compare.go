// Supervisor watch compare — the cross-check between the daemon-side
// supervisor watch's own findings and the findings the oracle script
// (scripts/supervisor-watch.py --record <path>) publishes to its record
// file. One comparison per pass, recorded by the pass after its checks
// run: the two sides' kind+subject sets, the two one-sided diffs, and
// whether they agreed. The watch store keeps the last 500 comparisons
// plus a per-day agreement summary (the days of full agreement count),
// persisted next to the finding records in supervisor_watch_record.go.
//
// A missing record file, a stale one (older than three times the
// supervisor watch poll interval), and an unparseable one all mean
// "script output unavailable" — not a disagreement. Such a pass records
// no comparison at all, so the agreement history never learns about a
// missing oracle as if the oracle had said something else.
package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SupervisorWatchComparisonLen caps the comparison history the watch
// store keeps: the last 500 comparisons.
const SupervisorWatchComparisonLen = 500

// supervisorWatchScriptStale is the age at which the oracle script's
// record file no longer counts: older than three times the supervisor
// watch poll interval (supervisorWatchMinPollInterval, the guard the
// pass in supervisor_watch_pass.go keeps between consecutive passes).
const supervisorWatchScriptStale = 3 * supervisorWatchMinPollInterval

// SupervisorWatchComparison — the result of one pass's comparison
// between the daemon-side supervisor watch's findings and the oracle
// script's record file. WatchKinds and ScriptKinds are the two sides'
// kind+subject sets (sorted, de-duplicated); OnlyWatch and OnlyScript
// hold the one-sided differences; Agreed is true exactly when both
// differences are empty.
type SupervisorWatchComparison struct {
	// At is when the pass ran the comparison.
	At time.Time `json:"at"`
	// WatchKinds is the daemon-side supervisor watch's kind+subject set.
	WatchKinds []string `json:"watch_kinds"`
	// ScriptKinds is the oracle script's kind+subject set.
	ScriptKinds []string `json:"script_kinds"`
	// OnlyWatch carries the kind+subject entries the watch raised that
	// the script did not.
	OnlyWatch []string `json:"only_watch,omitempty"`
	// OnlyScript carries the kind+subject entries the script raised that
	// the watch did not.
	OnlyScript []string `json:"only_script,omitempty"`
	// Agreed reports whether the two sides raised exactly the same
	// kind+subject set.
	Agreed bool `json:"agreed"`
}

// SupervisorWatchComparisonFile is the JSON file the watch store writes
// its comparison history and per-day agreement summary to, next to
// supervisor_watch.jsonl under the already-created directory.
const SupervisorWatchComparisonFile = "supervisor_watch_comparisons.json"

// supervisorWatchCompareLabel renders one finding's kind+subject pair as
// its label in a comparison: the kind, a slash, the subject (which may
// be empty, in which case the label ends at the slash).
func supervisorWatchCompareLabel(kind SupervisorWatchKind, subject string) string {
	return string(kind) + "/" + subject
}

// uniqueSorted de-duplicates and sorts in so the comparison records the
// set in a stable order; an empty in reads as an empty (non-nil) set.
func uniqueSorted(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// setDifference returns the entries of a that are not in b.
func setDifference(a, b []string) []string {
	inB := make(map[string]bool, len(b))
	for _, s := range b {
		inB[s] = true
	}
	var out []string
	for _, s := range a {
		if !inB[s] {
			out = append(out, s)
		}
	}
	return out
}

// supervisorWatchCompareSets builds one comparison from the two sides'
// kind+subject label sets. Sets, not sequences: duplicates and order on
// either side are irrelevant; the two sides agree exactly when the two
// one-sided differences are both empty.
func supervisorWatchCompareSets(watch, script []string) SupervisorWatchComparison {
	w := uniqueSorted(watch)
	s := uniqueSorted(script)
	onlyWatch := setDifference(w, s)
	onlyScript := setDifference(s, w)
	return SupervisorWatchComparison{
		WatchKinds:  w,
		ScriptKinds: s,
		OnlyWatch:   onlyWatch,
		OnlyScript:  onlyScript,
		Agreed:      len(onlyWatch) == 0 && len(onlyScript) == 0,
	}
}

// supervisorWatchScriptFinding — one finding the oracle script recorded
// under its --record option (kind is one of the supervisor watch's
// stable finding kinds, subject the id the finding is about or empty,
// text the line the script printed).
type supervisorWatchScriptFinding struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
	Text    string `json:"text"`
}

// supervisorWatchScriptRecord — the top-level shape of the oracle
// script's record file: the writing instant (unix seconds) and the
// findings (an empty list when the script found nothing).
type supervisorWatchScriptRecord struct {
	At       int64                          `json:"at"`
	Findings []supervisorWatchScriptFinding `json:"findings"`
}

// loadSupervisorWatchScriptRecord reads and decodes the oracle
// script's record file at path; any read or decode failure is an error
// to the caller (which treats it as "unavailable").
func loadSupervisorWatchScriptRecord(path string) (supervisorWatchScriptRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return supervisorWatchScriptRecord{}, err
	}
	var rec supervisorWatchScriptRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return supervisorWatchScriptRecord{}, fmt.Errorf("supervisor watch: parse script record %s: %w", path, err)
	}
	return rec, nil
}

// supervisorWatchScriptFresh reports whether the record file was written
// recently enough to be comparable against this pass: a record with a
// missing instant, or one older than supervisorWatchScriptStale (three
// poll intervals), is "script output unavailable", not a disagreement.
func supervisorWatchScriptFresh(rec supervisorWatchScriptRecord, now time.Time) bool {
	if rec.At <= 0 {
		return false
	}
	return now.Sub(time.Unix(rec.At, 0)) <= supervisorWatchScriptStale
}

// supervisorWatchCompare cross-checks this pass's findings against the
// oracle script's record file (the configured ScriptOutputPath) and
// records the comparison in the same watch store the findings live in.
// It returns the recorded comparison, or nil when the pass had nothing
// to record: no script output path configured, a missing or stale
// record file, an unparseable one, or no store. Unavailable is never
// recorded as disagreement.
func (m *Manager) supervisorWatchCompare(cfg SupervisorWatchConfig, watch []SupervisorWatchFinding, now time.Time) *SupervisorWatchComparison {
	if m == nil {
		return nil
	}
	if strings.TrimSpace(cfg.ScriptOutputPath) == "" {
		return nil
	}
	rec, err := loadSupervisorWatchScriptRecord(cfg.ScriptOutputPath)
	if err != nil || !supervisorWatchScriptFresh(rec, now) {
		return nil
	}

	watchSet := make([]string, 0, len(watch))
	for _, f := range watch {
		if f.Kind == "" {
			continue
		}
		watchSet = append(watchSet, supervisorWatchCompareLabel(f.Kind, f.Subject))
	}
	scriptSet := make([]string, 0, len(rec.Findings))
	for _, f := range rec.Findings {
		if f.Kind == "" {
			continue
		}
		scriptSet = append(scriptSet, f.Kind+"/"+f.Subject)
	}

	cmp := supervisorWatchCompareSets(watchSet, scriptSet)
	cmp.At = now

	st := m.supervisorWatchStore()
	if st == nil {
		return nil
	}
	out, err := st.RecordComparison(cmp)
	if err != nil {
		return nil
	}
	return out
}

// --- store side: history + per-day agreement ---------------------------
// (The SupervisorWatchStore itself lives in supervisor_watch_record.go;
// it gains the comparison history and day summary here, alongside its
// finding records.)

// supervisorWatchDayAgreement tallies one calendar day's comparisons:
// how many ran and how many disagreed.
type supervisorWatchDayAgreement struct {
	Total     int `json:"total"`
	Disagreed int `json:"disagreed"`
}

// Full reports whether every comparison of the day agreed; a day with
// no comparisons is not a full-agreement day.
func (d supervisorWatchDayAgreement) Full() bool {
	return d.Total > 0 && d.Disagreed == 0
}

// SupervisorWatchDaySummary is the per-day agreement summary the watch
// store keeps: calendar day (local time) -> tally.
type SupervisorWatchDaySummary struct {
	Days map[string]supervisorWatchDayAgreement `json:"days"`
}

// FullAgreementDays counts the days of the summary whose comparisons all
// agreed — the "days of full agreement count" the agreement history
// exists to show.
func (s SupervisorWatchDaySummary) FullAgreementDays() int {
	n := 0
	for _, d := range s.Days {
		if d.Full() {
			n++
		}
	}
	return n
}

// loadComparisons reads the comparison history file if one exists; a
// missing file reads as an empty history, a corrupt one is an error.
func (s *SupervisorWatchStore) loadComparisons() error {
	b, err := os.ReadFile(filepath.Join(s.dir, SupervisorWatchComparisonFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var file struct {
		Comparisons []SupervisorWatchComparison            `json:"comparisons"`
		Days        map[string]supervisorWatchDayAgreement `json:"days"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		return fmt.Errorf("supervisor watch: parse comparison history %s: %w", SupervisorWatchComparisonFile, err)
	}
	s.compareMu.Lock()
	s.comparisons = append([]SupervisorWatchComparison(nil), file.Comparisons...)
	s.compareDays = make(map[string]supervisorWatchDayAgreement, len(file.Days))
	for k, v := range file.Days {
		s.compareDays[k] = v
	}
	s.compareMu.Unlock()
	return nil
}

// compareDayKey renders the calendar day (local time) a comparison
// belongs to.
func compareDayKey(at time.Time) string {
	return at.Local().Format("2006-01-02")
}

// supervisorWatchCompareFile is the shape of the persistence file: the
// history plus the per-day tallies.
type supervisorWatchCompareFile struct {
	Comparisons []SupervisorWatchComparison            `json:"comparisons"`
	Days        map[string]supervisorWatchDayAgreement `json:"days"`
}

// persistComparisons writes the history and per-day tallies to
// SupervisorWatchComparisonFile (temp file + rename, so a torn write
// never lands on the live file).
func (s *SupervisorWatchStore) persistComparisons(file supervisorWatchCompareFile) error {
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	target := filepath.Join(s.dir, SupervisorWatchComparisonFile)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, target)
}

// RecordComparison stores one comparison in the history (keeping only
// the last SupervisorWatchComparisonLen, 500), applies it to the
// per-day agreement summary, and persists both. It returns a copy of
// the recorded comparison; a persist failure rolls the change back and
// returns the error.
func (s *SupervisorWatchStore) RecordComparison(c SupervisorWatchComparison) (*SupervisorWatchComparison, error) {
	in := c
	if in.WatchKinds == nil {
		in.WatchKinds = []string{}
	}
	if in.ScriptKinds == nil {
		in.ScriptKinds = []string{}
	}
	if in.OnlyWatch == nil {
		in.OnlyWatch = []string{}
	}
	if in.OnlyScript == nil {
		in.OnlyScript = []string{}
	}

	s.compareMu.Lock()
	snap := supervisorWatchCompareFile{
		Comparisons: append([]SupervisorWatchComparison(nil), s.comparisons...),
		Days:        make(map[string]supervisorWatchDayAgreement, len(s.compareDays)),
	}
	for k, v := range s.compareDays {
		snap.Days[k] = v
	}
	s.comparisons = append(s.comparisons, in)
	for len(s.comparisons) > SupervisorWatchComparisonLen {
		s.comparisons = s.comparisons[1:]
	}
	day := compareDayKey(in.At)
	tally := s.compareDays[day]
	tally.Total++
	if !in.Agreed {
		tally.Disagreed++
	}
	s.compareDays[day] = tally
	newFile := supervisorWatchCompareFile{
		Comparisons: append([]SupervisorWatchComparison(nil), s.comparisons...),
		Days:        map[string]supervisorWatchDayAgreement{},
	}
	for k, v := range s.compareDays {
		newFile.Days[k] = v
	}
	s.compareMu.Unlock()

	if err := s.persistComparisons(newFile); err != nil {
		s.compareMu.Lock()
		s.comparisons = snap.Comparisons
		s.compareDays = snap.Days
		s.compareMu.Unlock()
		return nil, err
	}
	out := in
	return &out, nil
}

// ListComparisons returns copies of the stored comparison history,
// oldest first; an empty history reads as an empty (non-nil) list.
func (s *SupervisorWatchStore) ListComparisons() []SupervisorWatchComparison {
	s.compareMu.Lock()
	defer s.compareMu.Unlock()
	out := make([]SupervisorWatchComparison, len(s.comparisons))
	copy(out, s.comparisons)
	return out
}

// DayAgreement returns a copy of the per-day agreement summary.
func (s *SupervisorWatchStore) DayAgreement() SupervisorWatchDaySummary {
	s.compareMu.Lock()
	defer s.compareMu.Unlock()
	out := SupervisorWatchDaySummary{Days: make(map[string]supervisorWatchDayAgreement, len(s.compareDays))}
	for k, v := range s.compareDays {
		out.Days[k] = v
	}
	return out
}
