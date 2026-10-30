// Supervisor watch record — one open finding the daemon-side supervisor
// watch has raised (or would raise, in shadow mode) and is tracking until
// an operator resolves it. Mirrors the repo CI watch record and store:
// a separate, small, mutex-guarded store persisted as JSONL under the
// already-created <data_dir>/autonomous/ directory, loaded at startup
// and saved on every change; a missing file reads as empty.
//
// On each coordinator poll, with no model call, the daemon raises
// through the existing alert and notify channels, and records here,
// the findings scripts/supervisor-watch.py produces. In shadow mode
// (the release default) the watch records, never acts.
package autonomous

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SupervisorWatchKind is the stable finding vocabulary of the supervisor
// watch. The string values are the on-the-wire and on-disk names the
// watch publishes, so they must not change; new kinds are additive.
type SupervisorWatchKind string

// The supervisor watch finding kinds — exactly this set.
const (
	// SupervisorWatchTmuxRestart — the tmux server on a node restarted
	// or became unreadable; the sessions its non-terminal Automata ran
	// in need an audit.
	SupervisorWatchTmuxRestart SupervisorWatchKind = "tmux_restart"
	// SupervisorWatchDaemonInactive — the Automaton daemon on a node is
	// not active.
	SupervisorWatchDaemonInactive SupervisorWatchKind = "daemon_inactive"
	// SupervisorWatchModelNotResident — the working model is not
	// resident on a node that should serve it.
	SupervisorWatchModelNotResident SupervisorWatchKind = "model_not_resident"
	// SupervisorWatchTaskFailed — a task of an Automaton failed.
	SupervisorWatchTaskFailed SupervisorWatchKind = "task_failed"
	// SupervisorWatchTaskReviewNeeded — a task of an Automaton is
	// waiting for review.
	SupervisorWatchTaskReviewNeeded SupervisorWatchKind = "task_review_needed"
	// SupervisorWatchAutomatonNotMerged — an Automaton finished all of
	// its work but has not been merged.
	SupervisorWatchAutomatonNotMerged SupervisorWatchKind = "automaton_not_merged"
	// SupervisorWatchIdleLaneEligibleWork — a lane is idle while work
	// that is eligible to run is waiting.
	SupervisorWatchIdleLaneEligibleWork SupervisorWatchKind = "idle_lane_eligible_work"
	// SupervisorWatchWorktreeNoGomod — an Automaton's worktree has no
	// go.mod; its project directory is broken.
	SupervisorWatchWorktreeNoGomod SupervisorWatchKind = "worktree_no_gomod"
	// SupervisorWatchWorktreeNotRegistered — an Automaton's worktree is
	// not registered.
	SupervisorWatchWorktreeNotRegistered SupervisorWatchKind = "worktree_not_registered"
	// SupervisorWatchWorktreeSilent — an Automaton's worktree went
	// silent while its worker was expected to be active.
	SupervisorWatchWorktreeSilent SupervisorWatchKind = "worktree_silent"
	// SupervisorWatchVerifyScriptNotExecutable — an Automaton's verify
	// script is not executable, so its gate could not run.
	SupervisorWatchVerifyScriptNotExecutable SupervisorWatchKind = "verify_script_not_executable"
	// SupervisorWatchMainCheckoutDirty — the main checkout carries
	// uncommitted tracked files; a ship would abort.
	SupervisorWatchMainCheckoutDirty SupervisorWatchKind = "main_checkout_dirty"
	// SupervisorWatchCIRed — a CI run is red.
	SupervisorWatchCIRed SupervisorWatchKind = "ci_red"
	// SupervisorWatchCISTuck — a CI run has been in progress abnormally
	// long.
	SupervisorWatchCISTuck SupervisorWatchKind = "ci_stuck"
)

// SupervisorWatchKinds is the complete stable finding vocabulary, in
// declaration order.
var SupervisorWatchKinds = []SupervisorWatchKind{
	SupervisorWatchTmuxRestart,
	SupervisorWatchDaemonInactive,
	SupervisorWatchModelNotResident,
	SupervisorWatchTaskFailed,
	SupervisorWatchTaskReviewNeeded,
	SupervisorWatchAutomatonNotMerged,
	SupervisorWatchIdleLaneEligibleWork,
	SupervisorWatchWorktreeNoGomod,
	SupervisorWatchWorktreeNotRegistered,
	SupervisorWatchWorktreeSilent,
	SupervisorWatchVerifyScriptNotExecutable,
	SupervisorWatchMainCheckoutDirty,
	SupervisorWatchCIRed,
	SupervisorWatchCISTuck,
}

// ValidSupervisorWatchKind reports whether k is one of the stable
// finding kinds. The empty kind is not valid: every stored finding must
// say what it is about.
func ValidSupervisorWatchKind(k SupervisorWatchKind) bool {
	for _, known := range SupervisorWatchKinds {
		if k == known {
			return true
		}
	}
	return false
}

// SupervisorWatchFinding is one open (or newly resolved) supervisor
// watch finding. Kind and Subject are the record's identity: Subject is
// the stable id the finding is about (an Automaton id, a node name, a
// lane name, a CI run id, or the main checkout's path), so the same
// condition on the same subject upserts the same record instead of
// minting a new one.
type SupervisorWatchFinding struct {
	// Kind is what the finding is about (see SupervisorWatchKinds).
	Kind SupervisorWatchKind `json:"kind"`
	// Subject is the stable id the finding is about: an Automaton id, a
	// node name, a lane name, a CI run id, or the main checkout's path.
	Subject string `json:"subject"`
	// Text is the operator-facing one line for this finding (built by
	// the supervisor watch text builder); single line, no tracker ids.
	Text string `json:"text,omitempty"`
	// FirstSeenAt is when the finding was first raised; it is preserved
	// across refreshes of the same kind+subject.
	FirstSeenAt time.Time `json:"first_seen_at"`
	// LastSeenAt is when the finding was last re-raised or touched.
	LastSeenAt time.Time `json:"last_seen_at"`
	// AlertedAt is nil until an alert for the finding has been raised.
	// Shadow mode never sets this: it records WouldAlert instead.
	AlertedAt *time.Time `json:"alerted_at,omitempty"`
	// WouldAlert is true from the moment the pass, running in shadow,
	// found this finding alert-due: shadow records what it would have
	// raised (with WouldAlertAt) instead of calling the alert sink. It
	// is false in on mode (which alerts for real and records
	// AlertedAt) and off mode (which records nothing).
	WouldAlert bool `json:"would_alert,omitempty"`
	// WouldAlertAt is when the pass first marked WouldAlert on this
	// episode (shadow mode); zero when the finding was never marked.
	WouldAlertAt time.Time `json:"would_alert_at,omitempty"`
	// Resolved flips to true when an operator (or, later, an automated
	// pass) closes the finding; a refresh after resolution re-opens it
	// in place, keeping its FirstSeenAt history.
	Resolved bool `json:"resolved"`
}

// SupervisorWatchKey composes the composite key identifying one finding:
// the finding kind plus its subject. The unit-separator byte cannot
// appear in either field, so the join is lossless and no two distinct
// findings collide.
func SupervisorWatchKey(kind SupervisorWatchKind, subject string) string {
	return string(kind) + "\x1f" + subject
}

// Key returns the composite key for this finding's identity fields.
func (f *SupervisorWatchFinding) Key() string {
	return SupervisorWatchKey(f.Kind, f.Subject)
}

// supervisorWatchValidateFinding checks the invariants a finding needs
// before it can be stored: a known kind, a non-empty subject, and a
// single-line Text when one is set — the text is operator-facing
// (alerts and the record store) and must stay one line of English.
func supervisorWatchValidateFinding(f *SupervisorWatchFinding) error {
	if f == nil {
		return fmt.Errorf("supervisor watch: nil finding")
	}
	if !ValidSupervisorWatchKind(f.Kind) {
		return fmt.Errorf("supervisor watch: unknown kind %q (must be one of %s)",
			f.Kind, strings.Join(supervisorWatchKindStrings(), ", "))
	}
	if strings.TrimSpace(f.Subject) == "" {
		return fmt.Errorf("supervisor watch: subject is required")
	}
	if strings.ContainsAny(f.Text, "\r\n") {
		return fmt.Errorf("supervisor watch: text must be one line: %q", f.Text)
	}
	return nil
}

// supervisorWatchKindStrings renders the kind vocabulary for error
// messages.
func supervisorWatchKindStrings() []string {
	out := make([]string, 0, len(SupervisorWatchKinds))
	for _, k := range SupervisorWatchKinds {
		out = append(out, string(k))
	}
	return out
}

// SupervisorWatchFileName is the JSONL file the store persists under the
// already-created <data_dir>/autonomous/ directory, next to
// repo_ci_watch.jsonl.
const SupervisorWatchFileName = "supervisor_watch.jsonl"

// SupervisorWatchStore is the in-memory + JSONL-backed store for
// supervisor watch findings, keyed by SupervisorWatchKind+Subject. A
// missing file means an empty store; every change re-persists the whole
// list (same full-rewrite approach as RepoCIWatchStore).
type SupervisorWatchStore struct {
	// mu guards findings.
	mu       sync.Mutex
	dir      string
	findings map[string]*SupervisorWatchFinding // keyed by SupervisorWatchKey
	// persistMu serializes file writes so concurrent persist calls don't
	// interleave on the same file.
	persistMu sync.Mutex
}

// NewSupervisorWatchStore opens (or creates) supervisor_watch.jsonl
// under dir — the already-created <data_dir>/autonomous/ directory. A
// missing file reads as an empty store.
func NewSupervisorWatchStore(dir string) (*SupervisorWatchStore, error) {
	s := &SupervisorWatchStore{
		dir:      dir,
		findings: map[string]*SupervisorWatchFinding{},
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SupervisorWatchStore) load() error {
	err := loadJSONL(filepath.Join(s.dir, SupervisorWatchFileName), func(line []byte) error {
		var f SupervisorWatchFinding
		if err := json.Unmarshal(line, &f); err != nil {
			return err
		}
		s.findings[f.Key()] = &f
		return nil
	})
	return err
}

// cloneSupervisorWatchFinding returns a deep copy of f via a JSON round
// trip so callers cannot mutate store state through the copy; a nil
// finding stays nil.
func cloneSupervisorWatchFinding(f *SupervisorWatchFinding) *SupervisorWatchFinding {
	if f == nil {
		return nil
	}
	b, err := json.Marshal(f)
	if err != nil {
		panic(err) // supervisor watch findings are plain data; marshaling cannot fail
	}
	var out SupervisorWatchFinding
	if err := json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return &out
}

// Get returns a deep copy of the finding under key; the second result
// reports whether it exists.
func (s *SupervisorWatchStore) Get(key string) (*SupervisorWatchFinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.findings[key]
	if !ok {
		return nil, false
	}
	return cloneSupervisorWatchFinding(f), true
}

// List returns deep copies of all findings: open findings first, most
// recently seen first, resolved findings last, most recently resolved
// first.
func (s *SupervisorWatchStore) List() []*SupervisorWatchFinding {
	s.mu.Lock()
	n := len(s.findings)
	out := make([]*SupervisorWatchFinding, 0, n)
	for _, f := range s.findings {
		out = append(out, cloneSupervisorWatchFinding(f))
	}
	s.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Resolved != out[j].Resolved {
			return !out[i].Resolved
		}
		if !out[i].LastSeenAt.Equal(out[j].LastSeenAt) {
			return out[i].LastSeenAt.After(out[j].LastSeenAt)
		}
		return out[i].Key() < out[j].Key()
	})
	return out
}

// persist writes the in-memory state back as JSON-lines (full rewrite),
// the same approach as RepoCIWatchStore.persist.
func (s *SupervisorWatchStore) persist() error {
	s.mu.Lock()
	n := len(s.findings)
	snapshot := make([]*SupervisorWatchFinding, 0, n)
	for _, f := range s.findings {
		snapshot = append(snapshot, f)
	}
	s.mu.Unlock()

	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	return writeJSONL(filepath.Join(s.dir, SupervisorWatchFileName), n, func(emit func(any) error) error {
		for _, f := range snapshot {
			if err := emit(f); err != nil {
				return err
			}
		}
		return nil
	})
}

// Upsert stores the finding under its kind+subject key and persists the
// change, returning a deep copy of the stored finding. A refresh of an
// existing finding keeps its FirstSeenAt, its AlertedAt unless the
// incoming finding carries a newer one, and the shadow WouldAlert mark
// (and its earliest WouldAlertAt); a fresh observation re-opens the
// finding in place (Resolved false) so the same condition does not mint
// records forever — a re-open after resolution is a new episode, so its
// alert history starts clean (AlertedAt nil, WouldAlert cleared) and
// the pass may alert for it again, or in shadow mark it alert-due
// again. A persist failure rolls the in-memory change back and returns
// the persist error.
func (s *SupervisorWatchStore) Upsert(f *SupervisorWatchFinding) (*SupervisorWatchFinding, error) {
	if err := supervisorWatchValidateFinding(f); err != nil {
		return nil, err
	}
	key := f.Key()
	now := time.Now()

	s.mu.Lock()
	stored, existed := s.findings[key]
	snap := cloneSupervisorWatchFinding(stored)
	in := cloneSupervisorWatchFinding(f)
	if in.FirstSeenAt.IsZero() {
		in.FirstSeenAt = now
	}
	if existed {
		if !snap.FirstSeenAt.IsZero() {
			in.FirstSeenAt = snap.FirstSeenAt
		}
		if snap.Resolved {
			// A fresh observation after the finding was closed is a new
			// episode: it starts its own alert history, so the pass can
			// alert for it again (or, in shadow, mark it alert-due
			// again).
			in.AlertedAt = nil
			in.WouldAlert = false
			in.WouldAlertAt = time.Time{}
		} else {
			// Same episode still open: a refresh never un-records an
			// already-raised alert or an already-recorded shadow mark.
			if snap.AlertedAt != nil && (in.AlertedAt == nil || snap.AlertedAt.After(*in.AlertedAt)) {
				in.AlertedAt = snap.AlertedAt
			}
			if snap.WouldAlert {
				in.WouldAlert = true
				if snap.WouldAlertAt.After(in.WouldAlertAt) {
					in.WouldAlertAt = snap.WouldAlertAt
				}
			}
		}
	}
	in.LastSeenAt = now
	in.Resolved = false
	s.findings[key] = in
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		s.mu.Lock()
		if existed {
			s.findings[key] = snap
		} else {
			delete(s.findings, key)
		}
		s.mu.Unlock()
		return nil, err
	}
	out, ok := s.Get(key)
	if !ok {
		return nil, fmt.Errorf("supervisor watch: finding %q disappeared after upsert", key)
	}
	return out, nil
}

// Resolve marks the finding under key resolved (refreshing its
// LastSeenAt) and persists the change, returning a deep copy of the
// stored finding. A missing finding is an error.
func (s *SupervisorWatchStore) Resolve(key string) (*SupervisorWatchFinding, error) {
	s.mu.Lock()
	stored, ok := s.findings[key]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("supervisor watch: no finding for key %q", key)
	}
	snap := cloneSupervisorWatchFinding(stored)
	now := time.Now()
	stored.Resolved = true
	stored.LastSeenAt = now
	s.mu.Unlock()

	if err := s.persist(); err != nil {
		s.mu.Lock()
		s.findings[key] = snap
		s.mu.Unlock()
		return nil, err
	}
	out, ok := s.Get(key)
	if !ok {
		return nil, fmt.Errorf("supervisor watch: finding %q disappeared after resolve", key)
	}
	return out, nil
}

// Delete removes the finding under key and persists the change; a
// missing key is an error.
func (s *SupervisorWatchStore) Delete(key string) error {
	s.mu.Lock()
	_, ok := s.findings[key]
	snap := cloneSupervisorWatchFinding(s.findings[key])
	delete(s.findings, key)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("supervisor watch: no finding for key %q", key)
	}

	if err := s.persist(); err != nil {
		s.mu.Lock()
		s.findings[key] = snap
		s.mu.Unlock()
		return err
	}
	return nil
}
