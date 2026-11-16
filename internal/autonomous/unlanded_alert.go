// Daily pending-branch alert: once per calendar day, when the number of
// pending Automaton branches (work that later work replaced and that waits
// for cleanup) is strictly above autonomous.unlanded_alert_threshold and
// autonomous.unlanded_alert_enabled is on, raise one operator alert naming
// the oldest pending branch and its age. At or below the threshold nothing
// is raised; with the alert off nothing is raised.
//
// The calendar day of the last alert is persisted next to the cleanup status
// in the Automata data folder, so a daemon restart on the same day does not
// repeat the alert. The check runs from the poll loop; it lists the branches
// (which reads git) at most once per unlandedAlertCheckEvery within a day and
// never again on a day that was already alerted.

package autonomous

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dmz006/datawatch/internal/config"
)

const (
	// unlandedAlertStateFile holds the persisted last-alerted day.
	unlandedAlertStateFile = "unlanded_alert.json"
	// unlandedAlertCheckEvery bounds how often a day that has not been
	// alerted yet re-lists the branches. A new calendar day always checks
	// at once.
	unlandedAlertCheckEvery = time.Hour
	// unlandedAlertDayLayout is the calendar-day key, in the clock's own
	// location (the daemon host's local time in production).
	unlandedAlertDayLayout = "2006-01-02"
)

// Outcomes of one unlanded alert check, reported in UnlandedAlertResult.
const (
	UnlandedAlertRaised         = "raised"
	UnlandedAlertDisabled       = "disabled"
	UnlandedAlertAlreadyToday   = "already_alerted_today"
	UnlandedAlertNotDue         = "not_due"
	UnlandedAlertAtOrBelow      = "at_or_below_threshold"
	UnlandedAlertListFailed     = "list_failed"
	unlandedAlertTitle          = "Pending Automaton branches above threshold"
	unlandedAlertUnknownAgeText = "an unknown time"
)

// UnlandedAlertResult is what one check decided.
type UnlandedAlertResult struct {
	// Outcome is one of the UnlandedAlert* outcome constants.
	Outcome   string `json:"outcome"`
	Day       string `json:"day,omitempty"`
	Pending   int    `json:"pending"`
	Threshold int    `json:"threshold"`
	// Oldest names the oldest pending branch when the alert was raised.
	Oldest *UnlandedOldestPending `json:"oldest,omitempty"`
}

// unlandedAlertState is the persisted record.
type unlandedAlertState struct {
	// LastAlertDay is the calendar day (YYYY-MM-DD) of the last alert.
	LastAlertDay string    `json:"last_alert_day,omitempty"`
	LastAlertAt  time.Time `json:"last_alert_at,omitempty"`
	LastPending  int       `json:"last_pending,omitempty"`
	LastBranch   string    `json:"last_branch,omitempty"`
}

// unlandedAlerter owns the once-a-day bookkeeping. It holds its own lock and
// is never guarded by the Manager's mu.
type unlandedAlerter struct {
	mu         sync.Mutex
	path       string
	now        func() time.Time
	checkEvery time.Duration
	lastCheck  time.Time
	st         unlandedAlertState
	// running keeps two poll ticks from listing at the same time.
	running atomic.Bool
}

func newUnlandedAlerter(dir string) *unlandedAlerter {
	a := &unlandedAlerter{
		path:       filepath.Join(dir, unlandedAlertStateFile),
		now:        time.Now,
		checkEvery: unlandedAlertCheckEvery,
	}
	a.load()
	return a
}

// load restores the persisted record. A missing file is the normal first
// start; a corrupt one is logged and dropped.
func (a *unlandedAlerter) load() {
	data, err := os.ReadFile(a.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[autonomous] unlanded alert: read %s: %v", a.path, err)
		}
		return
	}
	var st unlandedAlertState
	if err := json.Unmarshal(data, &st); err != nil {
		log.Printf("[autonomous] unlanded alert: %s is corrupt (%v); starting clean", a.path, err)
		return
	}
	a.st = st
}

// save persists the record. Caller holds a.mu. Returns the write error so
// the caller can log it; the alert itself has already been raised.
func (a *unlandedAlerter) save() error {
	data, err := json.Marshal(a.st)
	if err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

// lastAlertDay returns the persisted last-alerted day.
func (a *unlandedAlerter) lastAlertDay() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.st.LastAlertDay
}

// check runs one decision. list returns the current unlanded branches; alert
// raises the operator alert. Neither is called on a day already alerted, and
// list is not called while the alert is off.
func (a *unlandedAlerter) check(ctx context.Context, enabled bool, threshold int,
	list func(context.Context) ([]UnlandedBranch, error), alert func(title, body string)) UnlandedAlertResult {
	res := UnlandedAlertResult{Threshold: threshold}
	if !enabled {
		res.Outcome = UnlandedAlertDisabled
		return res
	}
	now := a.now()
	day := now.Format(unlandedAlertDayLayout)
	res.Day = day

	a.mu.Lock()
	if a.st.LastAlertDay == day {
		a.mu.Unlock()
		res.Outcome = UnlandedAlertAlreadyToday
		return res
	}
	if a.checkEvery > 0 && !a.lastCheck.IsZero() &&
		a.lastCheck.Format(unlandedAlertDayLayout) == day && now.Sub(a.lastCheck) < a.checkEvery {
		a.mu.Unlock()
		res.Outcome = UnlandedAlertNotDue
		return res
	}
	a.lastCheck = now
	a.mu.Unlock()

	branches, err := list(ctx)
	if err != nil {
		log.Printf("[autonomous] unlanded alert: list branches: %v", err)
		res.Outcome = UnlandedAlertListFailed
		return res
	}
	counts := CountUnlandedBranches(branches, now)
	res.Pending = counts.ByState[CountPending]
	if res.Pending <= threshold {
		res.Outcome = UnlandedAlertAtOrBelow
		return res
	}
	oldest := counts.OldestPending
	if oldest == nil {
		// Every pending branch lacks a last-commit time: still name one.
		for _, b := range branches {
			if LandingStateOf(b.Recommendation) == CountPending {
				title := b.Title
				if title == "" {
					title = b.AutomatonID
				}
				oldest = &UnlandedOldestPending{Branch: b.Branch, Title: title, AgeText: unlandedAlertUnknownAgeText}
				break
			}
		}
	}
	res.Oldest = oldest

	a.mu.Lock()
	// A second check that raced past the day test above must not alert twice.
	if a.st.LastAlertDay == day {
		a.mu.Unlock()
		res.Outcome = UnlandedAlertAlreadyToday
		return res
	}
	a.st = unlandedAlertState{LastAlertDay: day, LastAlertAt: now, LastPending: res.Pending}
	if oldest != nil {
		a.st.LastBranch = oldest.Branch
	}
	if err := a.save(); err != nil {
		log.Printf("[autonomous] unlanded alert: write %s: %v", a.path, err)
	}
	a.mu.Unlock()

	if alert != nil {
		alert(unlandedAlertTitle, unlandedAlertBody(res.Pending, threshold, oldest))
	}
	res.Outcome = UnlandedAlertRaised
	return res
}

// unlandedAlertBody renders the operator-facing alert text.
func unlandedAlertBody(pending, threshold int, oldest *UnlandedOldestPending) string {
	noun := "branches"
	if pending == 1 {
		noun = "branch"
	}
	body := fmt.Sprintf("%d pending Automaton %s wait for cleanup (threshold %d).", pending, noun, threshold)
	if oldest != nil {
		name := oldest.Branch
		if oldest.Title != "" && oldest.Title != oldest.Branch {
			name = fmt.Sprintf("%s (%s)", oldest.Branch, oldest.Title)
		}
		body += fmt.Sprintf(" The oldest is %s, last commit %s ago.", name, oldest.AgeText)
	}
	body += " Review them under unlanded work in Housekeeping, or with `datawatch autonomous unlanded-work`."
	return body
}

// validateUnlandedAlertPatch checks the two keys of an autonomous config
// write before anything is merged: a present enabled key must be a JSON
// boolean, a present threshold a whole number >= 0. null or absent passes.
func validateUnlandedAlertPatch(raw json.RawMessage) error {
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return nil // not an object: the merge below reports it
	}
	if v, ok := top["unlanded_alert_enabled"]; ok && string(v) != "null" {
		var b bool
		if json.Unmarshal(v, &b) != nil {
			return fmt.Errorf("autonomous.unlanded_alert_enabled: must be a JSON boolean (true or false)")
		}
	}
	if v, ok := top["unlanded_alert_threshold"]; ok && string(v) != "null" {
		var n int
		if json.Unmarshal(v, &n) != nil {
			return fmt.Errorf("autonomous.unlanded_alert_threshold must be a whole number >= 0 (default is %d)",
				config.DefaultUnlandedAlertThreshold)
		}
		if err := config.ValidateUnlandedAlertThreshold(&n); err != nil {
			return err
		}
	}
	return nil
}

// ── Manager surface ────────────────────────────────────────────────────

// UnlandedAlertCheck runs the daily pending-branch check once, synchronously,
// with the settings in effect now. It is safe to call on every poll tick.
func (m *Manager) UnlandedAlertCheck(ctx context.Context) UnlandedAlertResult {
	if m == nil || m.unlandedAlert == nil {
		return UnlandedAlertResult{Outcome: UnlandedAlertDisabled}
	}
	cfg := m.Config()
	enabled := config.UnlandedAlertEnabledEffective(cfg.UnlandedAlertEnabled)
	threshold := config.UnlandedAlertThresholdEffective(cfg.UnlandedAlertThreshold)
	list := m.unlandedAlertList
	if list == nil {
		list = m.UnlandedWork
	}
	return m.unlandedAlert.check(ctx, enabled, threshold, list, func(title, body string) {
		m.raiseUnlandedAlert(title, body, threshold)
	})
}

// startUnlandedAlertCheck runs UnlandedAlertCheck on its own goroutine so a
// slow git listing never blocks the poll loop; a check still running makes
// this a no-op.
func (m *Manager) startUnlandedAlertCheck(ctx context.Context) {
	if m == nil || m.unlandedAlert == nil {
		return
	}
	if !m.unlandedAlert.running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer m.unlandedAlert.running.Store(false)
		m.UnlandedAlertCheck(ctx)
	}()
}

// raiseUnlandedAlert sends the alert through the operator alert sink and
// records it in the audit log.
func (m *Manager) raiseUnlandedAlert(title, body string, threshold int) {
	if fn := m.AlertSink(); fn != nil {
		fn("warn", title, body)
	}
	m.mu.Lock()
	auditFn := m.auditFn
	m.mu.Unlock()
	if auditFn != nil {
		auditFn("autonomous_unlanded_alert", map[string]any{
			"threshold": threshold,
			"day":       m.unlandedAlert.lastAlertDay(),
		})
	}
}
