// dependency_update_pass.go — the coordinator's weekly dependency update
// pass. Once per schedule instant it instantiates the built-in
// 'dependency_update' template into one Automaton.
//
// One pass per idle-loop tick (automation_idle.go calls
// c.dependencyUpdatePass() beside securityFailurePass and
// supervisorWatchPass). It does nothing when
// autonomous.automation.dependency_update.enabled is false, and at most
// one poll runs per dependencyUpdateMinPollInterval. When the schedule is
// due (DependencyUpdateDue, in the daemon's local zone) and the Automaton
// of the previous run is finished, it creates the new Automaton with
// max_majors as a template variable and records the time and the new
// Automaton's title as the last run. The record is written next to the
// other watch stores, so a restart does not run the same instant twice.
//
// While the previous Automaton from this schedule is not finished the pass
// starts nothing and notes the skip in the Events record (the audit sink);
// the run is not lost, the next poll after that Automaton finishes
// starts it. The pass never reads, changes or removes any other Automaton
// or any pull request.
package autonomous

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// dependencyUpdateMinPollInterval is the minimum time between two polls of
// the pass; the idle loop ticks every minute and a schedule is weekly.
const dependencyUpdateMinPollInterval = time.Minute

// dependencyUpdateStateFile is the persisted state, in the store directory.
const dependencyUpdateStateFile = "dependency_update_state.json"

// dependencyUpdateState is the persisted record of the pass.
type dependencyUpdateState struct {
	// Baseline is the instant the pass first saw the schedule on this
	// installation; it stands in for a last run until one exists, so the
	// first start never runs on its own.
	Baseline time.Time `json:"baseline,omitempty"`
	// LastRun is when the last run was started; zero means none yet.
	LastRun time.Time `json:"last_run,omitempty"`
	// Title is the title of the Automaton the last run created.
	Title string `json:"title,omitempty"`
	// AutomatonID is that Automaton's id, kept to tell whether it finished.
	AutomatonID string `json:"automaton_id,omitempty"`
	// Reported is true once the finished run's outcome was recorded.
	Reported bool `json:"reported,omitempty"`
	// History holds the reported runs, newest last, capped.
	History []DependencyUpdateRunReport `json:"history,omitempty"`
}

// dependencyUpdateHistoryLimit caps the stored reported runs.
const dependencyUpdateHistoryLimit = 12

// DependencyUpdateRunInfo is the read view of the pass for the status tool.
type DependencyUpdateRunInfo struct {
	// At is when the last run happened; zero when there is none.
	At time.Time
	// AutomatonTitle is the title of the Automaton that run created.
	AutomatonTitle string
	// Finished reports whether that Automaton has ended (or is gone).
	Finished bool
	// Status is that Automaton's current status text; empty when unknown.
	Status string
	// Next is the next scheduled run; zero when the Automation is off or
	// the schedule cannot be read.
	Next time.Time
}

func (c *Coordinator) dependencyUpdateZone() *time.Location {
	if c.dependencyUpdateLoc != nil {
		return c.dependencyUpdateLoc
	}
	return time.Local
}

// dependencyUpdateFinished reports whether the Automaton with this id is
// done: terminal, or no longer in the store.
func (c *Coordinator) dependencyUpdateFinished(id string) (finished bool, status string) {
	if id == "" {
		return true, ""
	}
	prd, ok := c.m.store.GetPRD(id)
	if !ok {
		return true, ""
	}
	var st PRDStatus
	c.m.readLocked(func() { st = prd.Status })
	switch st {
	case PRDCompleted, PRDFailed, PRDCancelled, PRDRejected, PRDArchived:
		return true, string(st)
	}
	return false, string(st)
}

// dependencyUpdateLoad reads the persisted state once (caller holds c.mu).
func (c *Coordinator) dependencyUpdateLoad() {
	if c.dependencyUpdateLoaded {
		return
	}
	c.dependencyUpdateLoaded = true
	data, err := os.ReadFile(filepath.Join(c.m.store.dir, dependencyUpdateStateFile))
	if err != nil {
		return
	}
	var st dependencyUpdateState
	if json.Unmarshal(data, &st) == nil {
		c.dependencyUpdateRun = st
	}
}

// dependencyUpdateSave writes the state through a temp file and a rename.
func (c *Coordinator) dependencyUpdateSave(st dependencyUpdateState) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	path := filepath.Join(c.m.store.dir, dependencyUpdateStateFile)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// dependencyUpdatePass is the pass described in the file header.
func (c *Coordinator) dependencyUpdatePass() {
	if c == nil || c.m == nil || c.m.store == nil {
		return
	}
	cfg := c.m.Config().Automation.DependencyUpdate
	if !cfg.EffectiveEnabled() {
		return
	}
	now := c.now()
	c.dependencyUpdateReportPass(now)

	c.mu.Lock()
	if !c.dependencyUpdateLastPoll.IsZero() && now.Sub(c.dependencyUpdateLastPoll) < dependencyUpdateMinPollInterval {
		c.mu.Unlock()
		return
	}
	c.dependencyUpdateLastPoll = now
	c.dependencyUpdateLoad()
	st := c.dependencyUpdateRun
	c.mu.Unlock()

	if st.LastRun.IsZero() && st.Baseline.IsZero() {
		st.Baseline = now
		c.dependencyUpdateStore(st)
		return
	}
	since := st.LastRun
	if since.IsZero() {
		since = st.Baseline
	}
	due, err := DependencyUpdateDue(cfg.EffectiveSchedule(), since, now, c.dependencyUpdateZone())
	if err != nil {
		c.audit("dependency_update_schedule_invalid", map[string]any{"error": err.Error()})
		return
	}
	if !due {
		return
	}
	if finished, status := c.dependencyUpdateFinished(st.AutomatonID); !finished {
		c.mu.Lock()
		noted := c.dependencyUpdateSkipNoted
		c.dependencyUpdateSkipNoted = true
		c.mu.Unlock()
		if noted {
			return // one note per held run, not one per poll
		}
		c.audit("dependency_update_skipped", map[string]any{
			"reason":    "the previous dependency update Automaton is not finished",
			"automaton": st.Title,
			"status":    status,
		})
		return
	}

	vars := map[string]string{"max_majors": strconv.Itoa(cfg.EffectiveMaxMajors())}
	prd, err := c.m.InstantiateFromTemplateStore(TemplateDependencyUpdate, vars, "", "", "")
	if err != nil {
		c.audit("dependency_update_failed", map[string]any{"error": err.Error()})
		return
	}
	title := prd.Title
	if title == "" {
		title = TemplateDependencyUpdate
	}
	st.LastRun, st.Title, st.AutomatonID, st.Reported = now, title, prd.ID, false
	c.mu.Lock()
	c.dependencyUpdateSkipNoted = false
	c.mu.Unlock()
	c.dependencyUpdateStore(st)
	c.audit("dependency_update_started", map[string]any{"automaton": title})
}

// dependencyUpdateStore keeps st in memory and writes it to disk.
func (c *Coordinator) dependencyUpdateStore(st dependencyUpdateState) {
	c.mu.Lock()
	c.dependencyUpdateRun = st
	c.mu.Unlock()
	if err := c.dependencyUpdateSave(st); err != nil {
		c.audit("dependency_update_state_unsaved", map[string]any{"error": fmt.Sprint(err)})
	}
}

// DependencyUpdateRun returns the last run and the next scheduled run for
// the status tool. A nil coordinator reads as no run.
func (c *Coordinator) DependencyUpdateRun() DependencyUpdateRunInfo {
	if c == nil || c.m == nil || c.m.store == nil {
		return DependencyUpdateRunInfo{}
	}
	c.mu.Lock()
	c.dependencyUpdateLoad()
	st := c.dependencyUpdateRun
	c.mu.Unlock()
	info := DependencyUpdateRunInfo{At: st.LastRun, AutomatonTitle: st.Title}
	if !st.LastRun.IsZero() {
		info.Finished, info.Status = c.dependencyUpdateFinished(st.AutomatonID)
	}
	cfg := c.m.Config().Automation.DependencyUpdate
	if cfg.EffectiveEnabled() {
		if next, err := NextDependencyUpdateRun(cfg.EffectiveSchedule(), c.now(), c.dependencyUpdateZone()); err == nil {
			info.Next = next
		}
	}
	return info
}
